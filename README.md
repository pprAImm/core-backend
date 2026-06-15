# Core Backend — prAIm

Backend-сервис платформы ИИ-сериалов. Предоставляет REST API для категорий, сериалов, эпизодов, комментариев и рейтингов.

## Архитектура

```
Фронтенд (порт 3000) → Gateway (порт 8081) → Core Backend (порт 8080) → PostgreSQL (порт 5432)
```

Gateway проксирует запросы с префиксом `/api/*` в бэкенд (без префикса).

## Технологии

- **Go 1.26+**
- **PostgreSQL 18+**
- **Chi** — роутер
- **sqlc** — генерация Go-кода из SQL
- **oapi-codegen** — генерация сервера из OpenAPI-спецификации
- **bcrypt** — хеширование паролей
- **pgx** — драйвер PostgreSQL

## Структура проекта

```
core-backend/
├── api/                       # OpenAPI спецификации
│   ├── openapi.yaml
│   ├── components/            # Схемы, параметры, ответы
│   └── paths/                 # Эндпоинты
├── cmd/
│   └── server/main.go         # Точка входа
├── internal/
│   └── api/
│       ├── api.gen.go         # Сгенерировано из OpenAPI
│       ├── server.go          # Реализация хендлеров
│       └── middleware.go      # AuthMiddleware (сессионная cookie)
├── Makefile
├── go.mod
└── go.sum
```

## База данных

Схема и миграции находятся в директории `database/`:

```
database/
├── sql/
│   ├── migrations/            # Миграции (goose)
│   ├── queries/               # SQL-запросы для sqlc
│   └── test.sql               # Тестовые данные
├── internal/db/               # Сгенерированный sqlc код
├── store/                     # Слой доступа к данным (Store interface)
└── docker-compose.yml         # PostgreSQL для разработки
```

## Запуск

### 1. PostgreSQL

```bash
docker compose -f database/docker-compose.yml up -d
```

Учётные данные по умолчанию: `admin` / `1` / `series`.

### 2. Миграции и тестовые данные

```bash
export DATABASE_URL="postgres://admin:1@localhost:5432/series?sslmode=disable"
goose -dir database/sql/migrations postgres "$DATABASE_URL" up
psql "$DATABASE_URL" -f database/sql/test.sql
```

### 3. Backend

```bash
cd core-backend
#создаём файл .env для подключения к существующей бд 
cat > .env << 'EOF'
DATABASE_URL=postgres://team5:ПАРОЛЬ@212.8.228.70:5432/event?sslmode=require
EOF

go run ./cmd/server/
```

Сервер запустится на `http://localhost:8080`.

### 4. Gateway

```bash
cd gateway
go run .
```

Gateway запустится на `http://localhost:8081`, проксируя `/api/*` → backend `:8080/*`.

## API Endpoints

### Публичные (не требуют авторизации)

| Метод | URL (через gateway) | URL (напрямую в backend) | Описание |
|-------|---------------------|--------------------------|----------|
| GET | `/api/categories` | `/categories` | Список категорий |
| GET | `/api/categories/{slug}` | `/categories/{slug}` | Категория с сериалами |
| GET | `/api/series/search?q=` | `/series/search?q=` | Поиск сериалов (без q= возвращает всё) |
| GET | `/api/series/{id}` | `/series/{id}` | Сериал с эпизодами |
| GET | `/api/series/{id}/rating` | `/series/{id}/rating` | Средний рейтинг |
| GET | `/api/series/{id}/comments` | `/series/{id}/comments` | Комментарии |
| POST | `/api/auth/login` | `/auth/login` | Вход |
| POST | `/api/auth/register` | `/auth/register` | Регистрация |

### Защищённые (требуют сессионную cookie)

| Метод | URL | Описание |
|-------|-----|----------|
| POST | `/api/auth/logout` | Выход |
| GET | `/api/auth/me` | Текущий пользователь |
| POST | `/api/series/{id}/rating` | Поставить/обновить оценку (1–10) |
| POST | `/api/series/{id}/comments` | Добавить комментарий |

### Коды ответов

| Код | Описание |
|-----|----------|
| 200 | Успех |
| 201 | Создано |
| 400 | Неверный запрос |
| 401 | Не авторизован |
| 404 | Не найдено |
| 409 | Конфликт (email уже существует) |

## Примеры запросов

```bash
# Регистрация
curl -X POST http://localhost:8081/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"user","email":"user@test.com","password":"123456"}'

# Вход
curl -X POST http://localhost:8081/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@test.com","password":"123456"}' \
  -c cookies.txt

# Текущий пользователь
curl http://localhost:8081/api/auth/me -b cookies.txt

# Все категории
curl http://localhost:8081/api/categories

# Сериал по ID
curl http://localhost:8081/api/series/1

# Поиск
curl "http://localhost:8081/api/series/search?q=фруктовый"

# Оценка
curl -X POST http://localhost:8081/api/series/1/rating \
  -H "Content-Type: application/json" \
  -d '{"score":9}' \
  -b cookies.txt

# Комментарий
curl -X POST http://localhost:8081/api/series/1/comments \
  -H "Content-Type: application/json" \
  -d '{"body":"Отличный сериал!"}' \
  -b cookies.txt

# Выход
curl -X POST http://localhost:8081/api/auth/logout -b cookies.txt
```

## Команды Makefile

```bash
make install-tools  # Установка инструментов (oapi-codegen, sqlc)
make generate       # Генерация кода из OpenAPI
make build          # Сборка бинарника
make run            # Запуск сервера
make test           # Тесты
make lint           # Линтер
make clean          # Очистка сгенерированного кода
```
