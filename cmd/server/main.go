package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"

	"github.com/pprAImm/core-backend/internal/api"
	"github.com/pprAImm/core-backend/internal/mailer"
	"github.com/pprAImm/database"
	"github.com/pprAImm/database/store"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	log.Println("Запуск сервера...")

	if err := godotenv.Load(); err != nil {
		log.Println("Предупреждение: .env файл не найден")
	} else {
		log.Println(".env файл загружен успешно")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Println("DATABASE_URL не найден в окружении")
	} else {
		if len(dbURL) > 50 {
			log.Printf("DATABASE_URL найден: %s...", dbURL[:50])
		} else {
			log.Printf("DATABASE_URL найден: %s", dbURL)
		}
	}

	pool, err := database.Init()
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}
	defer pool.Close()
	log.Println("Подключение к БД установлено")

	queries := database.NewQueries(pool)
	storeInstance := store.NewStore(queries)
	mailerInstance := mailer.NewFromEnv()

	if mailerInstance.IsConfigured() {
		log.Printf("SMTP сконфигурирован: %s:%s", os.Getenv("SMTP_HOST"), os.Getenv("SMTP_PORT"))
	} else {
		log.Println("SMTP не сконфигурирован — письма подтверждения не будут отправляться")
	}

	server := api.NewServer(storeInstance, mailerInstance, pool)
	strictHandler := api.NewStrictHandler(server, nil)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://127.0.0.1:3000", "http://localhost:8080", "http://127.0.0.1:8081", "http://localhost:8081"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	// Popular & new series endpoints (must be before /series/{id})
	r.Get("/series/popular", func(w http.ResponseWriter, r *http.Request) {
		rows, err := pool.Query(r.Context(), `
			SELECT s.id, s.title, s.description, s.cover_url,
			       ROUND(COALESCE(AVG(r.rating), 0)::numeric, 1)::float8 as average_rating,
			       COUNT(r.id)::bigint as vote_count
			FROM series s
			LEFT JOIN ratings r ON r.series_id = s.id
			GROUP BY s.id
			ORDER BY
			  (ROUND(COALESCE(AVG(r.rating), 0)::numeric, 1)::float8 * COUNT(r.id)::float8)
			  / (COUNT(r.id)::float8 + 10) DESC
			LIMIT 16
		`)
		if err != nil {
			log.Printf("ListPopularSeries: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Не удалось загрузить популярные сериалы"})
			return
		}
		defer rows.Close()
		result := []map[string]interface{}{}
		for rows.Next() {
			var id, voteCount int64
			var title string
			var description, coverUrl *string
			var averageRating float64
			if err := rows.Scan(&id, &title, &description, &coverUrl, &averageRating, &voteCount); err != nil {
				log.Printf("scan popular: %v", err)
				continue
			}
			result = append(result, map[string]interface{}{
				"id":             id,
				"title":          title,
				"description":    description,
				"cover_url":      coverUrl,
				"average_rating": averageRating,
				"vote_count":     voteCount,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	r.Get("/series/new", func(w http.ResponseWriter, r *http.Request) {
		rows, err := pool.Query(r.Context(), `
			SELECT s.id, s.title, s.description, s.cover_url,
			       ROUND(COALESCE(AVG(r.rating), 0)::numeric, 1)::float8 as average_rating,
			       COUNT(r.id)::bigint as vote_count
			FROM series s
			LEFT JOIN ratings r ON r.series_id = s.id
			GROUP BY s.id
			ORDER BY s.id DESC
		`)
		if err != nil {
			log.Printf("ListNewSeries: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Не удалось загрузить новые сериалы"})
			return
		}
		defer rows.Close()
		result := []map[string]interface{}{}
		for rows.Next() {
			var id, voteCount int64
			var title string
			var description, coverUrl *string
			var averageRating float64
			if err := rows.Scan(&id, &title, &description, &coverUrl, &averageRating, &voteCount); err != nil {
				log.Printf("scan new series: %v", err)
				continue
			}
			result = append(result, map[string]interface{}{
				"id":             id,
				"title":          title,
				"description":    description,
				"cover_url":      coverUrl,
				"average_rating": averageRating,
				"vote_count":     voteCount,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	// POST /auth/verify/resend — повторная отправка письма подтверждения
	r.Post("/auth/verify/resend", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Укажите email"})
			return
		}

		user, err := storeInstance.GetUserByEmail(r.Context(), body.Email)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Пользователь с таким email не найден"})
			return
		}

		var emailVerified bool
		if err := pool.QueryRow(r.Context(), "SELECT email_verified FROM users WHERE id = $1", user.ID).Scan(&emailVerified); err == nil && emailVerified {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Email уже подтверждён"})
			return
		}

		tokenBytes := make([]byte, 16)
		if _, err := rand.Read(tokenBytes); err != nil {
			http.Error(w, `{"error":"Внутренняя ошибка сервера"}`, http.StatusInternalServerError)
			return
		}
		verificationToken := hex.EncodeToString(tokenBytes)

		tokenExpiresAt := time.Now().Add(24 * time.Hour)
		if _, err := pool.Exec(r.Context(), "UPDATE users SET verification_token = $1, verification_token_expires_at = $2 WHERE id = $3", verificationToken, tokenExpiresAt, user.ID); err != nil {
			log.Printf("ResendVerification: failed to save token: %v", err)
			http.Error(w, `{"error":"Не удалось создать токен"}`, http.StatusInternalServerError)
			return
		}

		if mailerInstance != nil && mailerInstance.IsConfigured() {
			verifyURL := os.Getenv("VERIFY_BASE_URL")
			if verifyURL == "" {
				verifyURL = "http://localhost:3000"
			}
			go func() {
				if err := mailerInstance.SendVerificationEmail(user.Email, user.Username, verificationToken, verifyURL); err != nil {
					log.Printf("Ошибка отправки письма подтверждения для %s: %v", user.Email, err)
				} else {
					log.Printf("Письмо подтверждения отправлено на %s", user.Email)
				}
			}()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		} else {
			http.Error(w, `{"error":"SMTP не настроен"}`, http.StatusInternalServerError)
		}
	})

	// Serve uploaded files (covers, etc.)
	uploadDir := "./uploads"
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		log.Fatalf("Failed to create uploads directory: %v", err)
	}
	coverDir := filepath.Join(uploadDir, "covers")
	if err := os.MkdirAll(coverDir, 0755); err != nil {
		log.Fatalf("Failed to create covers directory: %v", err)
	}
	r.Handle("/uploads/*", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadDir))))

	handler := api.AuthMiddleware(storeInstance)(r)

	api.HandlerFromMux(strictHandler, r)

	// Кастомные эндпоинты профиля (вне OpenAPI spec)
	r.Put("/auth/me/username", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := api.GetUserIDFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"Требуется авторизация"}`, http.StatusUnauthorized)
			return
		}

		var body struct {
			Username string `json:"username"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"Неверный формат запроса"}`, http.StatusBadRequest)
			return
		}
		if body.Username == "" {
			http.Error(w, `{"error":"Имя не может быть пустым"}`, http.StatusBadRequest)
			return
		}

		user, err := storeInstance.UpdateUsername(r.Context(), userID, body.Username)
		if err != nil {
			http.Error(w, `{"error":"Не удалось обновить имя"}`, http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":       user.ID,
			"username": user.Username,
			"email":    user.Email,
		})
	})

	r.Put("/auth/me/password", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := api.GetUserIDFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"Требуется авторизация"}`, http.StatusUnauthorized)
			return
		}

		var body struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"Неверный формат запроса"}`, http.StatusBadRequest)
			return
		}
		if body.NewPassword == "" {
			http.Error(w, `{"error":"Новый пароль не может быть пустым"}`, http.StatusBadRequest)
			return
		}

		// Получаем текущий хеш пароля
		user, err := storeInstance.GetUserByIDWithPassword(r.Context(), userID)
		if err != nil {
			http.Error(w, `{"error":"Пользователь не найден"}`, http.StatusBadRequest)
			return
		}

		// Проверяем текущий пароль
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.CurrentPassword)); err != nil {
			http.Error(w, `{"error":"Неверный текущий пароль"}`, http.StatusBadRequest)
			return
		}

		// Хешируем новый пароль
		hashed, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, `{"error":"Внутренняя ошибка сервера"}`, http.StatusInternalServerError)
			return
		}

		if err := storeInstance.UpdatePassword(r.Context(), userID, string(hashed)); err != nil {
			http.Error(w, `{"error":"Не удалось обновить пароль"}`, http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// POST /series — создание нового сериала
	r.Post("/series", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := api.GetUserIDFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"Требуется авторизация"}`, http.StatusUnauthorized)
			return
		}

		if err := r.ParseMultipartForm(50 << 20); err != nil {
			http.Error(w, `{"error":"Не удалось разобрать форму"}`, http.StatusBadRequest)
			return
		}

		title := r.FormValue("title")
		if title == "" {
			http.Error(w, `{"error":"Название обязательно"}`, http.StatusBadRequest)
			return
		}
		description := r.FormValue("description")

		// Category slugs
		var categoryID *int64
		if slugsRaw := r.FormValue("category_slugs"); slugsRaw != "" {
			var slugs []string
			if err := json.Unmarshal([]byte(slugsRaw), &slugs); err == nil && len(slugs) > 0 {
				cat, err := storeInstance.GetCategoryBySlug(r.Context(), slugs[0])
				if err == nil {
					categoryID = &cat.ID
				}
			}
		}

		// Cover upload
		var coverURL *string
		file, header, err := r.FormFile("cover")
		if err == nil {
			defer file.Close()
			ext := filepath.Ext(header.Filename)
			token := make([]byte, 16)
			if _, e := rand.Read(token); e != nil {
				http.Error(w, `{"error":"Внутренняя ошибка"}`, http.StatusInternalServerError)
				return
			}
			filename := hex.EncodeToString(token) + ext
			dst, err := os.Create(filepath.Join(coverDir, filename))
			if err == nil {
				defer dst.Close()
				if _, err := io.Copy(dst, file); err == nil {
					url := "/uploads/covers/" + filename
					coverURL = &url
				}
			}
		}

		descPtr := &description
		if description == "" {
			descPtr = nil
		}

		series, err := storeInstance.CreateSeriesWithUploader(r.Context(), title, descPtr, categoryID, coverURL, &userID)
		if err != nil {
			log.Printf("CreateSeriesWithUploader: %v", err)
			http.Error(w, `{"error":"Не удалось создать сериал"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]int64{"id": series.ID})
	})

	// PUT /series/{id} — обновление сериала
	r.Put("/series/{id}", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := api.GetUserIDFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"Требуется авторизация"}`, http.StatusUnauthorized)
			return
		}
		_ = userID // could check ownership later

		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"Неверный ID"}`, http.StatusBadRequest)
			return
		}

		if err := r.ParseMultipartForm(50 << 20); err != nil {
			http.Error(w, `{"error":"Не удалось разобрать форму"}`, http.StatusBadRequest)
			return
		}

		title := r.FormValue("title")
		if title == "" {
			http.Error(w, `{"error":"Название обязательно"}`, http.StatusBadRequest)
			return
		}
		description := r.FormValue("description")

		var categoryID *int64
		if slugsRaw := r.FormValue("category_slugs"); slugsRaw != "" {
			var slugs []string
			if err := json.Unmarshal([]byte(slugsRaw), &slugs); err == nil && len(slugs) > 0 {
				cat, err := storeInstance.GetCategoryBySlug(r.Context(), slugs[0])
				if err == nil {
					categoryID = &cat.ID
				}
			}
		}

		var coverURL *string
		file, header, err := r.FormFile("cover")
		if err == nil {
			defer file.Close()
			ext := filepath.Ext(header.Filename)
			token := make([]byte, 16)
			if _, e := rand.Read(token); e != nil {
				http.Error(w, `{"error":"Внутренняя ошибка"}`, http.StatusInternalServerError)
				return
			}
			filename := hex.EncodeToString(token) + ext
			dst, err := os.Create(filepath.Join(coverDir, filename))
			if err == nil {
				defer dst.Close()
				if _, err := io.Copy(dst, file); err == nil {
					url := "/uploads/covers/" + filename
					coverURL = &url
				}
			}
		} else {
			// No new cover uploaded — keep existing cover_url
			existing, err := storeInstance.GetSeriesByID(r.Context(), id)
			if err == nil {
				coverURL = existing.CoverUrl
			}
		}

		descPtr := &description
		if description == "" {
			descPtr = nil
		}

		if _, err := storeInstance.UpdateSeries(r.Context(), id, title, descPtr, categoryID, coverURL); err != nil {
			log.Printf("UpdateSeries: %v", err)
			http.Error(w, `{"error":"Не удалось обновить сериал"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// GET /user/series — список сериалов текущего пользователя
	r.Get("/user/series", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := api.GetUserIDFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"Требуется авторизация"}`, http.StatusUnauthorized)
			return
		}
		rows, err := storeInstance.GetSeriesByUser(r.Context(), &userID)
		if err != nil {
			log.Printf("GetSeriesByUser: %v", err)
			http.Error(w, `{"error":"Не удалось загрузить сериалы"}`, http.StatusInternalServerError)
			return
		}
		result := make([]map[string]interface{}, 0, len(rows))
		for _, s := range rows {
			result = append(result, map[string]interface{}{
				"id":          s.ID,
				"title":       s.Title,
				"description": s.Description,
				"cover_url":   s.CoverUrl,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	// Запуск сервера
	log.Println("Сервер запущен на http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}
