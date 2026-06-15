# Core Backend - prAIm

Backend сервис для prAIm. Предоставляет REST API для работы с категориями, сериалами, эпизодами, комментариями и рейтингами.

## Технологии

- **Go 1.26+**
- **PostgreSQL 18+** - база данных
- **Chi** - роутер
- **sqlc** - генерация кода из SQL
- **oapi-codegen** - генерация сервера из OpenAPI
- **goose** - миграции
- **bcrypt** - хеширование паролей

## Структура проекта
core-backend/
├── api/ # OpenAPI спецификации
│ ├── openapi.yaml # Главная спецификация
│ ├── components/ # Компоненты (schemas, parameters, responses)
│ └── paths/ # Эндпоинты
├── cmd/
│ └── server/ # Точка входа
│ └── main.go
├── internal/
│ └── api/ # Сгенерированный код и реализация
│ ├── api_gen.go # Сгенерирован из OpenAPI
│ ├── server.go # Реализация хендлеров
│ └── middleware.go # Middleware авторизации
├── Makefile # Команды для сборки и запуска
├── go.mod
└── go.sum


## Установка и запуск

### 1. Клонирование репозитория

```bash
git clone https://github.com/pprAImm/core-backend.git
cd core-backend
2. Установка Go
Требуется Go версии 1.21 или выше:

bash
# Ubuntu/Debian
sudo apt update
sudo apt install golang-go

# Или скачать с официального сайта
wget https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
3. Установка инструментов
bash
# Установка goose для миграций
go install github.com/pressly/goose/v3/cmd/goose@latest

# Установка oapi-codegen
go install github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen@latest

# Установка sqlc
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

# Добавляем ~/go/bin в PATH
export PATH=$PATH:~/go/bin
4. Настройка базы данных
Локальная БД (Docker)
bash
# Запуск PostgreSQL в Docker
docker run --name postgres-db \
  -e POSTGRES_USER=admin \
  -e POSTGRES_PASSWORD=1 \
  -e POSTGRES_DB=series \
  -p 5432:5432 \
  -d postgres:15

# Применение миграций
cd ../database
export DATABASE_URL="postgres://admin:1@localhost:5432/series?sslmode=disable"
goose -dir sql/migrations postgres "$DATABASE_URL" up
Удалённая БД (сервер команды)
bash
# Создание .env файла с настройками подключения
cat > .env << 'EOF'
DATABASE_URL=postgres://team5:ПАРОЛЬ@212.8.228.70:5432/event?sslmode=require
EOF
Важно: Получите пароль у администратора. Замените ПАРОЛЬ и team5 на актуальные значения.

Миграции на удалённую БД
bash
cd ../database
export DATABASE_URL="postgres://team5:ПАРОЛЬ@212.8.228.70:5432/event?sslmode=require"
goose -dir sql/migrations postgres "$DATABASE_URL" up
5. Загрузка зависимостей
bash
cd ../core-backend
go mod download
go mod tidy
6. Запуск сервера
bash
# Создание .env файла (если не создан)
cat > .env << 'EOF'
DATABASE_URL=postgres://team5:ПАРОЛЬ@212.8.228.70:5432/event?sslmode=require
EOF

# Запуск
make run
# или
go run ./cmd/server/main.go
Сервер запустится на http://localhost:8080

Доступные Makefile команды
bash
make install-tools  # Установка всех инструментов разработки
make generate       # Генерация кода из OpenAPI
make build          # Сборка бинарного файла
make run            # Запуск сервера
make test           # Запуск тестов
make lint           # Проверка кода линтером
make clean          # Очистка сгенерированных файлов
make help           # Показать все команды
API Эндпоинты
Публичные (не требуют авторизации)
Метод	URL	Описание
GET	/categories	Список всех категорий
GET	/categories/{slug}	Категория по slug
GET	/series	Список сериалов
GET	/series/{id}	Сериал по ID с эпизодами
GET	/series/search?q={query}	Поиск сериалов
GET	/series/{id}/rating	Средний рейтинг сериала
GET	/series/{id}/comments	Комментарии к сериалу
POST	/auth/register	Регистрация
POST	/auth/login	Вход
Защищённые (требуют авторизации)
Метод	URL	Описание
POST	/auth/logout	Выход
GET	/auth/me	Текущий пользователь
POST	/series/{id}/rating	Поставить оценку
POST	/series/{id}/comments	Добавить комментарий
Коды ответов
Код	Описание
200	Успешный запрос
201	Ресурс создан
400	Неверный запрос
401	Не авторизован
404	Ресурс не найден
409	Конфликт (email уже существует)
Примеры запросов
bash
# Регистрация
curl -X POST http://localhost:8080/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"user","email":"user@test.com","password":"123456"}'

# Вход
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@test.com","password":"123456"}' \
  -c cookies.txt

# Получение текущего пользователя
curl -X GET http://localhost:8080/auth/me -b cookies.txt

# Все категории
curl http://localhost:8080/categories

# Сериал по ID
curl http://localhost:8080/series/1

# Поиск сериалов
curl "http://localhost:8080/series/search?q=фруктовый"

# Добавление комментария
curl -X POST http://localhost:8080/series/1/comments \
  -H "Content-Type: application/json" \
  -d '{"body":"Отличный сериал!"}' \
  -b cookies.txt

# Постановка оценки
curl -X POST http://localhost:8080/series/1/rating \
  -H "Content-Type: application/json" \
  -d '{"score":9}' \
  -b cookies.txt

# Выход
curl -X POST http://localhost:8080/auth/logout -b cookies.txt
Деплой на сервер
1. Клонирование на сервер
bash
ssh root@185.207.1.198
cd /root
git clone https://github.com/pprAImm/core-backend.git
cd core-backend
2. Настройка окружения
bash
cat > .env << 'EOF'
DATABASE_URL=postgres://team5:ПАРОЛЬ@212.8.228.70:5432/event?sslmode=require
EOF
3. Сборка и запуск
bash
go build -o bin/server ./cmd/server
nohup ./bin/server > server.log 2>&1 &
4. Настройка systemd (автозапуск)
bash
cat > /etc/systemd/system/core-backend.service << 'EOF'
[Unit]
Description=Core Backend Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/root/core-backend
EnvironmentFile=/root/core-backend/.env
ExecStart=/root/core-backend/bin/server
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable core-backend
systemctl start core-backend
systemctl status core-backend
Переменные окружения
Переменная	Описание	Пример
DATABASE_URL	URL подключения к PostgreSQL	postgres://user:pass@host:5432/db?sslmode=require