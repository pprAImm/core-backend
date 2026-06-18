package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

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

	handler := api.AuthMiddleware(storeInstance)(r)

	api.HandlerFromMux(strictHandler, r)

	// Profile endpoints
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

		user, err := storeInstance.GetUserByIDWithPassword(r.Context(), userID)
		if err != nil {
			http.Error(w, `{"error":"Пользователь не найден"}`, http.StatusBadRequest)
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.CurrentPassword)); err != nil {
			http.Error(w, `{"error":"Неверный текущий пароль"}`, http.StatusBadRequest)
			return
		}

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

	// POST /series — создание нового сериала (multipart)
	r.Post("/series", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := api.GetUserIDFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"Требуется авторизация"}`, http.StatusUnauthorized)
			return
		}

		if err := r.ParseMultipartForm(50 << 20); err != nil {
			http.Error(w, `{"error":"Неверный формат данных"}`, http.StatusBadRequest)
			return
		}

		title := r.FormValue("title")
		if title == "" {
			http.Error(w, `{"error":"Название обязательно"}`, http.StatusBadRequest)
			return
		}

		description := r.FormValue("description")

		categorySlugsJSON := r.FormValue("category_slugs")
		if categorySlugsJSON == "" {
			http.Error(w, `{"error":"Выберите хотя бы одну категорию"}`, http.StatusBadRequest)
			return
		}
		var categorySlugs []string
		if err := json.Unmarshal([]byte(categorySlugsJSON), &categorySlugs); err != nil || len(categorySlugs) == 0 {
			http.Error(w, `{"error":"Неверный формат категорий"}`, http.StatusBadRequest)
			return
		}

		// Берём первую категорию для category_id (series имеет одно category_id)
		category, err := storeInstance.GetCategoryBySlug(r.Context(), categorySlugs[0])
		if err != nil {
			http.Error(w, `{"error":"Категория не найдена"}`, http.StatusBadRequest)
			return
		}

		// Сохраняем обложку в БД как base64 data URL
		var coverURL *string
		coverFile, coverHeader, err := r.FormFile("cover")
		if err == nil {
			defer coverFile.Close()
			coverBytes, err := io.ReadAll(coverFile)
			if err != nil {
				log.Printf("read cover: %v", err)
				http.Error(w, `{"error":"Не удалось прочитать обложку"}`, http.StatusInternalServerError)
				return
			}
			mimeType := mime.TypeByExtension(filepath.Ext(coverHeader.Filename))
			if mimeType == "" {
				mimeType = "image/jpeg"
			}
			dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(coverBytes))
			coverURL = &dataURL
		} else {
			if urlStr := r.FormValue("cover_url"); urlStr != "" {
				coverURL = &urlStr
			}
		}

		// Создаём сериал
		var descPtr *string
		if description != "" {
			descPtr = &description
		}

		series, err := storeInstance.CreateSeriesWithUploader(r.Context(), title, descPtr, &category.ID, coverURL, &userID)
		if err != nil {
			log.Printf("create series: %v", err)
			http.Error(w, `{"error":"Не удалось создать сериал"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]int64{"id": series.ID})
	})

	// GET /user/series — список сериалов текущего пользователя
	r.Get("/user/series", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := api.GetUserIDFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"Требуется авторизация"}`, http.StatusUnauthorized)
			return
		}

		seriesList, err := storeInstance.GetSeriesByUser(r.Context(), &userID)
		if err != nil {
			log.Printf("get user series: %v", err)
			http.Error(w, `{"error":"Не удалось загрузить сериалы"}`, http.StatusInternalServerError)
			return
		}

		result := make([]map[string]interface{}, len(seriesList))
		for i, s := range seriesList {
			result[i] = map[string]interface{}{
				"id":          s.ID,
				"title":       s.Title,
				"description": s.Description,
				"cover_url":   s.CoverUrl,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	// PUT /series/{id} — обновление сериала (multipart, обложка опциональна)
	r.Put("/series/{id}", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := api.GetUserIDFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"Требуется авторизация"}`, http.StatusUnauthorized)
			return
		}

		seriesID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, `{"error":"Неверный ID сериала"}`, http.StatusBadRequest)
			return
		}

		existing, err := storeInstance.GetSeriesByID(r.Context(), seriesID)
		if err != nil {
			http.Error(w, `{"error":"Сериал не найден"}`, http.StatusNotFound)
			return
		}
		if existing.UploadedBy == nil || *existing.UploadedBy != userID {
			http.Error(w, `{"error":"Нет прав на редактирование"}`, http.StatusForbidden)
			return
		}

		if err := r.ParseMultipartForm(50 << 20); err != nil {
			http.Error(w, `{"error":"Неверный формат данных"}`, http.StatusBadRequest)
			return
		}

		title := r.FormValue("title")
		if title == "" {
			http.Error(w, `{"error":"Название обязательно"}`, http.StatusBadRequest)
			return
		}

		description := r.FormValue("description")

		categorySlugsJSON := r.FormValue("category_slugs")
		var categoryID *int64
		if categorySlugsJSON != "" {
			var categorySlugs []string
			if err := json.Unmarshal([]byte(categorySlugsJSON), &categorySlugs); err == nil && len(categorySlugs) > 0 {
				category, err := storeInstance.GetCategoryBySlug(r.Context(), categorySlugs[0])
				if err != nil {
					http.Error(w, `{"error":"Категория не найдена"}`, http.StatusBadRequest)
					return
				}
				categoryID = &category.ID
			}
		}

		// Обложка — если загружен новый файл, сохраняем как base64 data URL
		var coverURL *string
		coverFile, coverHeader, err := r.FormFile("cover")
		if err == nil {
			defer coverFile.Close()
			coverBytes, err := io.ReadAll(coverFile)
			if err != nil {
				log.Printf("read cover: %v", err)
				http.Error(w, `{"error":"Не удалось прочитать обложку"}`, http.StatusInternalServerError)
				return
			}
			mimeType := mime.TypeByExtension(filepath.Ext(coverHeader.Filename))
			if mimeType == "" {
				mimeType = "image/jpeg"
			}
			dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(coverBytes))
			coverURL = &dataURL
		} else {
			coverURL = existing.CoverUrl
		}

		var descPtr *string
		if description != "" {
			descPtr = &description
		}

		updated, err := storeInstance.UpdateSeries(r.Context(), seriesID, title, descPtr, categoryID, coverURL)
		if err != nil {
			log.Printf("update series: %v", err)
			http.Error(w, `{"error":"Не удалось обновить сериал"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":          updated.ID,
			"title":       updated.Title,
			"description": updated.Description,
			"cover_url":   updated.CoverUrl,
		})
	})

	// DELETE /series/{id} — удаление сериала (только владелец)
	r.Delete("/series/{id}", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := api.GetUserIDFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"Требуется авторизация"}`, http.StatusUnauthorized)
			return
		}

		seriesID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, `{"error":"Неверный ID сериала"}`, http.StatusBadRequest)
			return
		}

		existing, err := storeInstance.GetSeriesByID(r.Context(), seriesID)
		if err != nil {
			http.Error(w, `{"error":"Сериал не найден"}`, http.StatusNotFound)
			return
		}
		if existing.UploadedBy == nil || *existing.UploadedBy != userID {
			http.Error(w, `{"error":"Нет прав на удаление"}`, http.StatusForbidden)
			return
		}

		// Удаляем связанные записи (каскад вручную)
		if _, err := pool.Exec(r.Context(), `DELETE FROM comments WHERE series_id = $1`, seriesID); err != nil {
			log.Printf("delete comments: %v", err)
		}
		if _, err := pool.Exec(r.Context(), `DELETE FROM ratings WHERE series_id = $1`, seriesID); err != nil {
			log.Printf("delete ratings: %v", err)
		}
		if _, err := pool.Exec(r.Context(), `DELETE FROM episodes WHERE series_id = $1`, seriesID); err != nil {
			log.Printf("delete episodes: %v", err)
		}

		if _, err := storeInstance.DeleteSeries(r.Context(), seriesID); err != nil {
			log.Printf("delete series: %v", err)
			http.Error(w, `{"error":"Не удалось удалить сериал"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// DELETE /episodes/{id} — удаление эпизода (только владелец сериала)
	r.Delete("/episodes/{id}", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := api.GetUserIDFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"Требуется авторизация"}`, http.StatusUnauthorized)
			return
		}

		episodeID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, `{"error":"Неверный ID эпизода"}`, http.StatusBadRequest)
			return
		}

		episode, err := storeInstance.GetEpisodeByID(r.Context(), episodeID)
		if err != nil {
			http.Error(w, `{"error":"Эпизод не найден"}`, http.StatusNotFound)
			return
		}

		if episode.SeriesID == nil {
			http.Error(w, `{"error":"Эпизод не привязан к сериалу"}`, http.StatusBadRequest)
			return
		}

		series, err := storeInstance.GetSeriesByID(r.Context(), *episode.SeriesID)
		if err != nil {
			http.Error(w, `{"error":"Сериал не найден"}`, http.StatusNotFound)
			return
		}
		if series.UploadedBy == nil || *series.UploadedBy != userID {
			http.Error(w, `{"error":"Нет прав на удаление"}`, http.StatusForbidden)
			return
		}

		if _, err := storeInstance.DeleteEpisode(r.Context(), episodeID); err != nil {
			log.Printf("delete episode: %v", err)
			http.Error(w, `{"error":"Не удалось удалить эпизод"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// GET /api/auth/verify — подтверждение email по токену
	r.Get("/api/auth/verify", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "Токен не указан"})
			return
		}

		var emailVerified bool
		var username string
		err := pool.QueryRow(r.Context(),
			"SELECT email_verified, username FROM users WHERE verification_token = $1", token,
		).Scan(&emailVerified, &username)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "Неверный или просроченный токен"})
			return
		}

		if emailVerified {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "already_verified"})
			return
		}

		_, err = pool.Exec(r.Context(),
			"UPDATE users SET email_verified = true, verification_token = NULL WHERE verification_token = $1", token,
		)
		if err != nil {
			log.Printf("VerifyEmail: verify error: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(map[string]string{"error": "Не удалось подтвердить email"})
			return
		}

		log.Printf("VerifyEmail: email confirmed for user %s", username)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "verified"})
	})

	log.Println("Сервер запущен на http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}
