package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"core-backend/internal/api"
)

func main() {
	log.Println("Запуск сервера...")

	// Создаём сервер с реализацией хендлеров
	server := api.NewServer()

	// Оборачиваем в strict handler (для type safety)
	strictHandler := api.NewStrictHandler(server, nil)

	// Настройка роутера
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://localhost:8080"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	// Регистрируем все хендлеры из OpenAPI спецификации
	// HandlerFromMux автоматически добавляет все маршруты
	api.HandlerFromMux(strictHandler, r)

	// Запуск сервера
	log.Println("Сервер запущен на http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
