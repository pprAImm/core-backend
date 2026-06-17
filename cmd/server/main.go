package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"

	"github.com/pprAImm/core-backend/internal/api"
	"github.com/pprAImm/database"
	"github.com/pprAImm/database/store"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	log.Println("Запуск сервера...")

	// Загружаем .env файл
	if err := godotenv.Load(); err != nil {
		log.Println("Предупреждение: .env файл не найден")
	} else {
		log.Println(".env файл загружен успешно")
	}

	// Проверяем, что переменная установилась
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Println("⚠️ DATABASE_URL не найден в окружении")
	} else {
		// Выводим URL (скрывая пароль)
		if len(dbURL) > 50 {
			log.Printf("DATABASE_URL найден: %s...", dbURL[:50])
		} else {
			log.Printf("DATABASE_URL найден: %s", dbURL)
		}
	}

	// Подключение к базе данных
	pool, err := database.Init()
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}
	defer pool.Close()
	log.Println("Подключение к БД установлено")

	// Создание слоя доступа к данным
	queries := database.NewQueries(pool)
	storeInstance := store.NewStore(queries)

	// Создание сервера с реализацией хендлеров
	server := api.NewServer(storeInstance)
	strictHandler := api.NewStrictHandler(server, nil)

	// Настройка роутера
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://127.0.0.1:3000", "http://localhost:8080", "http://127.0.0.1:8081", "http://localhost:8081"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	// AuthMiddleware для авторизации
	handler := api.AuthMiddleware(storeInstance)(r)

	// Регистрация всех эндпоинтов
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

	// Запуск сервера
	log.Println("Сервер запущен на http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}
