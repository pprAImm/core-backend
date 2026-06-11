package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/pprAImm/core-backend/internal/api"

	"github.com/pprAImm/database"
	"github.com/pprAImm/database/store"
)

func main() {
	log.Println("Запуск сервера...")

	// 1. Подключение к базе данных
	// Инициализирует пул соединений с PostgreSQL
	pool, err := database.Init()
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}
	defer pool.Close() // Закрываем соединение при завершении
	log.Println("Подключение к БД установлено")

	// 2. Создание слоя доступа к данным
	// NewQueries - публичная фабрика из database/public.go
	// Store - слой бизнес-логики для работы с БД
	queries := database.NewQueries(pool)
	storeInstance := store.NewStore(queries)

	// 3. Создание HTTP сервера с реализацией хендлеров
	// NewServer принимает Store, чтобы хендлеры могли работать с БД
	server := api.NewServer(storeInstance)
	strictHandler := api.NewStrictHandler(server, nil)

	// 4. Настройка роутера Chi
	r := chi.NewRouter()

	// Middleware для логирования всех HTTP запросов
	r.Use(middleware.Logger)

	// Middleware для восстановления после паники (не даёт серверу упасть)
	r.Use(middleware.Recoverer)

	// Middleware для CORS (разрешает запросы с других доменов)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://localhost:8080"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	// 5. Регистрация всех эндпоинтов из OpenAPI спецификации
	// HandlerFromMux автоматически добавляет маршруты:
	// GET /categories, GET /series/{id}, POST /auth/login и т.д.
	api.HandlerFromMux(strictHandler, r)

	// 6. Запуск HTTP сервера на порту 8080
	log.Println("Сервер запущен на http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
