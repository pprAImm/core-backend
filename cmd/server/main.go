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

	// Глобальные middleware (применяются ко всем запросам)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://localhost:8080"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	// AuthMiddleware должен быть применён к роутеру ДО регистрации хендлеров
	// Оборачиваем весь роутер в AuthMiddleware
	rWithAuth := chi.NewRouter()
	rWithAuth.Use(api.AuthMiddleware(storeInstance))
	rWithAuth.Mount("/", r)

	// Регистрация всех эндпоинтов
	api.HandlerFromMux(strictHandler, r)

	// Запуск сервера (используем rWithAuth вместо r)
	log.Println("Сервер запущен на http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", rWithAuth))
}
