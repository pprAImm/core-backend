package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
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

	// POST /episodes/{id}/progress — сохранить прогресс просмотра
	r.Post("/episodes/{id}/progress", func(w http.ResponseWriter, r *http.Request) {
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

		var body struct {
			ProgressSeconds float64 `json:"progress_seconds"`
			DurationSeconds float64 `json:"duration_seconds"`
			Completed       bool    `json:"completed"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"Неверный формат запроса"}`, http.StatusBadRequest)
			return
		}

		_, err = pool.Exec(r.Context(), `
			INSERT INTO watch_progress (user_id, episode_id, progress_seconds, duration_seconds, completed, updated_at)
			VALUES ($1, $2, $3, $4, $5, now())
			ON CONFLICT (user_id, episode_id)
			DO UPDATE SET progress_seconds = $3, duration_seconds = $4, completed = $5, updated_at = now()
		`, userID, episodeID, body.ProgressSeconds, body.DurationSeconds, body.Completed)
		if err != nil {
			log.Printf("save watch progress: %v", err)
			http.Error(w, `{"error":"Не удалось сохранить прогресс"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// GET /episodes/{id}/progress — получить прогресс по одному эпизоду
	r.Get("/episodes/{id}/progress", func(w http.ResponseWriter, r *http.Request) {
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

		var progressSeconds, durationSeconds float64
		var completed bool
		err = pool.QueryRow(r.Context(), `
			SELECT progress_seconds, duration_seconds, completed
			FROM watch_progress
			WHERE user_id = $1 AND episode_id = $2
		`, userID, episodeID).Scan(&progressSeconds, &durationSeconds, &completed)

		if err != nil {
			progressSeconds = 0
			durationSeconds = 0
			completed = false
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"progress_seconds": progressSeconds,
			"duration_seconds": durationSeconds,
			"completed":       completed,
		})
	})

	// GET /series/{id}/progress — получить прогресс по всем эпизодам сериала
	r.Get("/series/{id}/progress", func(w http.ResponseWriter, r *http.Request) {
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

		rows, err := pool.Query(r.Context(), `
			SELECT wp.episode_id, wp.progress_seconds, wp.duration_seconds, wp.completed, wp.updated_at
			FROM watch_progress wp
			JOIN episodes e ON e.id = wp.episode_id
			WHERE wp.user_id = $1 AND e.series_id = $2
			ORDER BY e.episode_num
		`, userID, seriesID)
		if err != nil {
			log.Printf("get watch progress: %v", err)
			http.Error(w, `{"error":"Не удалось загрузить прогресс"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		result := []map[string]interface{}{}
		for rows.Next() {
			var episodeID int64
			var progressSeconds, durationSeconds float64
			var completed bool
			var updatedAt interface{}
			if err := rows.Scan(&episodeID, &progressSeconds, &durationSeconds, &completed, &updatedAt); err != nil {
				log.Printf("scan watch progress: %v", err)
				continue
			}
			result = append(result, map[string]interface{}{
				"episode_id":       episodeID,
				"progress_seconds": progressSeconds,
				"duration_seconds": durationSeconds,
				"completed":        completed,
				"updated_at":       updatedAt,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	log.Println("Сервер запущен на http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}
