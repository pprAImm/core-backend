package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/pprAImm/core-backend/internal/mailer"
	"github.com/pprAImm/database/store"
	"golang.org/x/crypto/bcrypt"
)

// Server - основная структура сервера, содержащая все обработчики API
type Server struct {
	Store  store.Store
	Mailer *mailer.Mailer
	Pool   *pgxpool.Pool // слой доступа к базе данных
}

// NewServer создаёт новый экземпляр сервера с переданным хранилищем
func NewServer(s store.Store, m *mailer.Mailer, p *pgxpool.Pool) *Server {
	return &Server{Store: s, Mailer: m, Pool: p}
}

// generateSessionID генерирует случайный 32-символьный идентификатор сессии в hex-формате
// Используется для создания уникального токена сессии при входе пользователя
func generateSessionID() (string, error) {
	bytes := make([]byte, 16)                   // 16 байт = 128 бит
	if _, err := rand.Read(bytes); err != nil { // заполняем случайными данными
		return "", err
	}
	return hex.EncodeToString(bytes), nil // преобразуем в hex-строку
}

// hashPassword хеширует пароль с помощью bcrypt для безопасного хранения в БД
// bcrypt автоматически добавляет соль и делает хеш устойчивым к перебору
func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// checkPasswordHash проверяет, соответствует ли введённый пароль сохранённому хешу
// Сравнивает пароль с хешем, возвращает true если пароль верный
func checkPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ==================== ЭНДПОЙНТЫ АВТОРИЗАЦИИ ====================

// Register обрабатывает POST /auth/register - регистрация нового пользователя
// Принимает email, пароль и имя пользователя, создаёт запись в таблице users
// Пароль хешируется перед сохранением
func (s *Server) Register(ctx context.Context, request RegisterRequestObject) (RegisterResponseObject, error) {
	log.Printf("POST /auth/register: %s", request.Body.Email)

	// Хешируем пароль перед сохранением в БД
	hashedPassword, err := hashPassword(request.Body.Password)
	if err != nil {
		return Register409JSONResponse{Error: "Внутренняя ошибка сервера"}, nil
	}

	// Создаем пользователя в базе данных
	// string(request.Body.Email) - преобразуем т.к. request.Body.Email имеет специальный тип
	user, err := s.Store.CreateUser(ctx, request.Body.Username, string(request.Body.Email), hashedPassword)
	if err != nil {
		return Register409JSONResponse{Error: "Пользователь с таким email уже существует"}, nil
	}

	// Генерируем токен подтверждения email
	verificationToken, err := generateSessionID()
	if err != nil {
		return Register409JSONResponse{Error: "Внутренняя ошибка сервера"}, nil
	}

	// Сохраняем токен в БД
	if verificationToken != "" {
		if _, err := s.Pool.Exec(ctx, "UPDATE users SET verification_token = $1 WHERE id = $2", verificationToken, user.ID); err != nil {
			log.Printf("Failed to save verification token: %v", err)
		}
	}

	// Создаём сессию (как в Login), чтобы пользователь сразу был авторизован
	sessionID, err := generateSessionID()
	if err != nil {
		return Register409JSONResponse{Error: "Не удалось создать сессию"}, nil
	}

	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	userID := user.ID
	_, err = s.Store.CreateSession(ctx, sessionID, &userID, expiresAt)
	if err != nil {
		return Register409JSONResponse{Error: "Не удалось создать сессию"}, nil
	}

	// Устанавливаем HttpOnly cookie для хранения ID сессии
	setCookie := "session_id=" + sessionID + "; HttpOnly; Path=/; Expires=" + expiresAt.Format(time.RFC1123)

	// Отправляем письмо с подтверждением (если SMTP настроен)
	if s.Mailer != nil && s.Mailer.IsConfigured() {
		verifyURL := os.Getenv("VERIFY_BASE_URL")
		if verifyURL == "" {
			verifyURL = "http://localhost:3000"
		}
		go func() {
			if err := s.Mailer.SendVerificationEmail(user.Email, user.Username, verificationToken, verifyURL); err != nil {
				log.Printf("Ошибка отправки письма подтверждения для %s: %v", user.Email, err)
			} else {
				log.Printf("Письмо подтверждения отправлено на %s", user.Email)
			}
		}()
	} else {
		log.Printf("SMTP не настроен, письмо подтверждения не отправлено для %s", user.Email)
	}

	// Возвращаем данные созданного пользователя (без пароля) и cookie с сессией
	return Register201JSONResponse{
		Body: struct {
			Email    openapi_types.Email `json:"email"`
			Id       int                 `json:"id"`
			Username string              `json:"username"`
		}{
			Email:    openapi_types.Email(user.Email),
			Id:       int(user.ID),
			Username: user.Username,
		},
		Headers: Register201ResponseHeaders{
			SetCookie: &setCookie,
		},
	}, nil
}

// Login обрабатывает POST /auth/login - вход пользователя и создание сессии
// Проверяет email и пароль, при успехе создаёт сессию и устанавливает cookie
func (s *Server) Login(ctx context.Context, request LoginRequestObject) (LoginResponseObject, error) {
	log.Printf("POST /auth/login: %s", request.Body.Email)

	// Ищем пользователя по email
	user, err := s.Store.GetUserByEmail(ctx, string(request.Body.Email))
	if err != nil {
		return Login401JSONResponse{Error: "Неверный email или пароль"}, nil
	}

	// Проверяем пароль (сравниваем введённый с хешем из БД)
	if !checkPasswordHash(request.Body.Password, user.PasswordHash) {
		return Login401JSONResponse{Error: "Неверный email или пароль"}, nil
	}

	// Генерируем уникальный ID для сессии
	sessionID, err := generateSessionID()
	if err != nil {
		return Login401JSONResponse{Error: "Не удалось создать сессию"}, nil
	}

	// Сохраняем сессию в БД (срок действия 7 дней)
	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	userID := user.ID
	_, err = s.Store.CreateSession(ctx, sessionID, &userID, expiresAt)
	if err != nil {
		return Login401JSONResponse{Error: "Не удалось создать сессию"}, nil
	}

	// Устанавливаем HTTP-only cookie для хранения ID сессии
	// HttpOnly - защита от XSS атак (JavaScript не может прочитать cookie)
	// Path=/ - cookie действует на всём сайте
	setCookie := "session_id=" + sessionID + "; HttpOnly; Path=/; Expires=" + expiresAt.Format(time.RFC1123)

	// Возвращаем данные пользователя и cookie
	return Login200JSONResponse{
		Body: struct {
			Email    openapi_types.Email `json:"email"`
			Id       int                 `json:"id"`
			Username string              `json:"username"`
		}{
			Email:    openapi_types.Email(user.Email),
			Id:       int(user.ID),
			Username: user.Username,
		},
		Headers: Login200ResponseHeaders{
			SetCookie: &setCookie,
		},
	}, nil
}

// GetCurrentUser обрабатывает GET /auth/me - получение текущего пользователя
func (s *Server) GetCurrentUser(ctx context.Context, request GetCurrentUserRequestObject) (GetCurrentUserResponseObject, error) {
	log.Println("GET /auth/me")

	// Получаем ID текущего пользователя из контекста
	userID, ok := GetUserIDFromContext(ctx)
	if !ok {
		return GetCurrentUser401JSONResponse{Error: "Требуется авторизация"}, nil
	}

	// Получаем данные пользователя из БД
	user, err := s.Store.GetUserByID(ctx, userID)
	if err != nil {
		log.Printf("GetCurrentUser: пользователь не найден: %v", err)
		return GetCurrentUser401JSONResponse{Error: "Пользователь не найден"}, nil
	}

	// Создаём указатели для полей (т.к. структура ожидает *int, *string)
	id := int(user.ID)
	username := user.Username
	email := user.Email

	// Возвращаем данные пользователя с указателями
	return GetCurrentUser200JSONResponse{
		Id:       &id,
		Username: &username,
		Email:    &email,
	}, nil
}

// Logout обрабатывает POST /auth/logout - выход пользователя и удаление сессии
// Удаляет сессию из БД, чтобы cookie стала невалидной
func (s *Server) Logout(ctx context.Context, request LogoutRequestObject) (LogoutResponseObject, error) {
	log.Println("POST /auth/logout")

	// Получаем ID сессии из контекста (установлен middleware AuthMiddleware)
	sessionID, ok := GetSessionIDFromContext(ctx)
	if !ok {
		// Если сессии нет, просто выходим
		return Logout200Response{}, nil
	}

	// Удаляем сессию из базы данных
	err := s.Store.DeleteSession(ctx, sessionID)
	if err != nil {
		log.Printf("Ошибка удаления сессии %s: %v", sessionID, err)
	} else {
		log.Printf("Сессия %s успешно удалена", sessionID)
	}

	// Возвращаем пустой ответ (браузер сам очистит cookie)
	return Logout200Response{}, nil
}

// ==================== ЭНДПОЙНТЫ КАТЕГОРИЙ ====================

// GetAllCategories обрабатывает GET /categories - получение списка всех категорий
// Используется на главной странице для отображения всех доступных категорий
func (s *Server) GetAllCategories(ctx context.Context, request GetAllCategoriesRequestObject) (GetAllCategoriesResponseObject, error) {
	log.Println("GET /categories")

	// Получаем категории из БД
	categories, err := s.Store.GetAllCategories(ctx)
	if err != nil {
		log.Printf("Ошибка получения категорий: %v", err)
		return GetAllCategories200JSONResponse{}, nil
	}

	// Конвертируем модели БД в формат ответа API
	result := make(GetAllCategories200JSONResponse, len(categories))
	for i, cat := range categories {
		result[i] = struct {
			Id   int    `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		}{
			Id:   int(cat.ID),
			Name: cat.Name,
			Slug: cat.Slug,
		}
	}

	return result, nil
}

// GetCategoryBySlug обрабатывает GET /categories/{slug} - получение категории и её сериалов
// Возвращает информацию о категории и список сериалов, принадлежащих этой категории
func (s *Server) GetCategoryBySlug(ctx context.Context, request GetCategoryBySlugRequestObject) (GetCategoryBySlugResponseObject, error) {
	log.Printf("GET /categories/%s", request.Slug)

	// Получаем категорию по slug (человекочитаемый идентификатор)
	category, err := s.Store.GetCategoryBySlug(ctx, request.Slug)
	if err != nil {
		return GetCategoryBySlug404JSONResponse{Error: "Категория не найдена"}, nil
	}

	// Получаем сериалы этой категории
	seriesList, err := s.Store.GetSeriesByCategory(ctx, &category.ID)
	if err != nil {
		// В случае ошибки возвращаем только категорию без сериалов
		result := GetCategoryBySlug200JSONResponse{
			Category: &struct {
				Id   int    `json:"id"`
				Name string `json:"name"`
				Slug string `json:"slug"`
			}{
				Id:   int(category.ID),
				Name: category.Name,
				Slug: category.Slug,
			},
		}
		return result, nil
	}

	// Формируем ответ с категорией
	result := GetCategoryBySlug200JSONResponse{
		Category: &struct {
			Id   int    `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		}{
			Id:   int(category.ID),
			Name: category.Name,
			Slug: category.Slug,
		},
	}

	// Конвертируем сериалы в формат ответа API (включая category_id и average_rating)
	if len(seriesList) > 0 {
		apiSeries := make([]struct {
			AverageRating *float32 `json:"average_rating,omitempty"`
			CategoryId    *int     `json:"category_id,omitempty"`
			CoverUrl      *string  `json:"cover_url,omitempty"`
			Description   *string  `json:"description,omitempty"`
			Id            int      `json:"id"`
			Title         string   `json:"title"`
		}, len(seriesList))

		for i, ser := range seriesList {
			// category id
			var catID *int
			if category.ID != 0 {
				v := int(category.ID)
				catID = &v
			}

			// average rating (optional)
			var avgPtr *float32
			if avgFloat, err := s.Store.GetAverageRating(ctx, &ser.ID); err == nil {
				v := float32(avgFloat)
				avgPtr = &v
			}

			apiSeries[i] = struct {
				AverageRating *float32 `json:"average_rating,omitempty"`
				CategoryId    *int     `json:"category_id,omitempty"`
				CoverUrl      *string  `json:"cover_url,omitempty"`
				Description   *string  `json:"description,omitempty"`
				Id            int      `json:"id"`
				Title         string   `json:"title"`
			}{
				Id:            int(ser.ID),
				Title:         ser.Title,
				Description:   ser.Description,
				CoverUrl:      ser.CoverUrl,
				CategoryId:    catID,
				AverageRating: avgPtr,
			}
		}
		result.Series = &apiSeries
	}

	return result, nil
}

// ==================== ЭНДПОЙНТЫ СЕРИАЛОВ ====================

// GetSeriesById обрабатывает GET /series/{id} - получение сериала с эпизодами
// Возвращает полную информацию о сериале и список всех его эпизодов
func (s *Server) GetSeriesById(ctx context.Context, request GetSeriesByIdRequestObject) (GetSeriesByIdResponseObject, error) {
	log.Printf("GET /series/%d", request.Id)

	// Получаем сериал по ID
	series, err := s.Store.GetSeriesByID(ctx, int64(request.Id))
	if err != nil {
		return GetSeriesById404JSONResponse{Error: "Сериал не найден"}, nil
	}

	// Получаем эпизоды сериала
	episodes, err := s.Store.GetEpisodesBySeries(ctx, &series.ID)
	if err != nil {
		// В случае ошибки возвращаем только сериал без эпизодов
		result := GetSeriesById200JSONResponse{
			Series: &struct {
				AverageRating *float32 `json:"average_rating,omitempty"`
				CategoryId    *int     `json:"category_id,omitempty"`
				CoverUrl      *string  `json:"cover_url,omitempty"`
				Description   *string  `json:"description,omitempty"`
				Id            int      `json:"id"`
				Title         string   `json:"title"`
			}{
				Id:            int(series.ID),
				Title:         series.Title,
				Description:   series.Description,
				CoverUrl:      series.CoverUrl,
				CategoryId:    nil,
				AverageRating: nil,
			},
		}
		return result, nil
	}

	// Формируем ответ с сериалом (включаем category_id и average_rating)
	result := GetSeriesById200JSONResponse{
		Series: &struct {
			AverageRating *float32 `json:"average_rating,omitempty"`
			CategoryId    *int     `json:"category_id,omitempty"`
			CoverUrl      *string  `json:"cover_url,omitempty"`
			Description   *string  `json:"description,omitempty"`
			Id            int      `json:"id"`
			Title         string   `json:"title"`
		}{
			Id:            int(series.ID),
			Title:         series.Title,
			Description:   series.Description,
			CoverUrl:      series.CoverUrl,
			CategoryId:    nil,
			AverageRating: nil,
		},
	}

	// average rating
	if avgFloat, err := s.Store.GetAverageRating(ctx, &series.ID); err == nil {
		v := float32(avgFloat)
		result.Series.AverageRating = &v
	}

	// Конвертируем эпизоды в формат ответа API
	if len(episodes) > 0 {
		apiEpisodes := make([]struct {
			EpisodeNum *int    `json:"episode_num,omitempty"`
			Id         int     `json:"id"`
			SeriesId   int     `json:"series_id"`
			TiktokUrl  string  `json:"tiktok_url"`
			Title      *string `json:"title,omitempty"`
		}, len(episodes))

		for i, ep := range episodes {
			var episodeNum *int
			if ep.EpisodeNum != nil {
				val := int(*ep.EpisodeNum)
				episodeNum = &val
			}

			var seriesID int
			if ep.SeriesID != nil {
				seriesID = int(*ep.SeriesID)
			}

			apiEpisodes[i] = struct {
				EpisodeNum *int    `json:"episode_num,omitempty"`
				Id         int     `json:"id"`
				SeriesId   int     `json:"series_id"`
				TiktokUrl  string  `json:"tiktok_url"`
				Title      *string `json:"title,omitempty"`
			}{
				Id:         int(ep.ID),
				SeriesId:   seriesID,
				Title:      ep.Title,
				TiktokUrl:  ep.TiktokUrl,
				EpisodeNum: episodeNum,
			}
		}
		result.Episodes = &apiEpisodes
	}

	return result, nil
}

// SearchSeries обрабатывает GET /series/search - поиск сериалов по названию
// Использует ILIKE для регистронезависимого поиска по части названия
func (s *Server) SearchSeries(ctx context.Context, request SearchSeriesRequestObject) (SearchSeriesResponseObject, error) {
	log.Printf("GET /series/search?q=%s", request.Params.Q)

	// Выполняем поиск в БД
	results, err := s.Store.SearchSeries(ctx, &request.Params.Q)
	if err != nil {
		log.Printf("Ошибка поиска: %v", err)
		return SearchSeries200JSONResponse{}, nil
	}

	// Конвертируем результаты в формат ответа API
	apiResults := make(SearchSeries200JSONResponse, len(results))
	for i, r := range results {
		apiResults[i] = struct {
			AverageRating *float32 `json:"average_rating,omitempty"`
			CategoryId    *int     `json:"category_id,omitempty"`
			CoverUrl      *string  `json:"cover_url,omitempty"`
			Description   *string  `json:"description,omitempty"`
			Id            int      `json:"id"`
			Title         string   `json:"title"`
		}{
			Id:            int(r.ID),
			Title:         r.Title,
			Description:   r.Description,
			CoverUrl:      r.CoverUrl,
			CategoryId:    nil,
			AverageRating: nil,
		}
	}

	return apiResults, nil
}

// ==================== ЭНДПОЙНТЫ КОММЕНТАРИЕВ ====================

// GetSeriesComments обрабатывает GET /series/{id}/comments - получение всех комментариев к сериалу
// Возвращает список комментариев, отсортированных от новых к старым
func (s *Server) GetSeriesComments(ctx context.Context, request GetSeriesCommentsRequestObject) (GetSeriesCommentsResponseObject, error) {
	log.Printf("GET /series/%d/comments", request.Id)

	seriesID := int64(request.Id)
	// Получаем комментарии из БД
	comments, err := s.Store.GetCommentsBySeries(ctx, &seriesID)
	if err != nil {
		return GetSeriesComments200JSONResponse{}, nil
	}

	// Конвертируем комментарии в формат ответа API
	apiComments := make(GetSeriesComments200JSONResponse, len(comments))
	for i, c := range comments {
		apiComments[i] = struct {
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"created_at"`
			Id        int       `json:"id"`
			Username  string    `json:"username"`
		}{
			Id:        int(c.ID),
			Username:  c.Username,
			Body:      c.Body,
			CreatedAt: c.CreatedAt,
		}
	}

	return apiComments, nil
}

// AddComment обрабатывает POST /series/{id}/comments - добавление нового комментария
// Требует авторизации (userID извлекается из контекста, установленного middleware)
// Возвращает созданный комментарий с реальным именем пользователя из БД
// AddComment обрабатывает POST /series/{id}/comments
func (s *Server) AddComment(ctx context.Context, request AddCommentRequestObject) (AddCommentResponseObject, error) {
	log.Printf("POST /series/%d/comments: %s", request.Id, request.Body.Body)

	// СНАЧАЛА проверяем авторизацию
	userID, ok := GetUserIDFromContext(ctx)
	if !ok {
		return AddComment401JSONResponse{Error: "Требуется авторизация"}, nil
	}

	// ПОТОМ проверяем валидность данных
	if request.Body.Body == "" {
		return AddComment400JSONResponse{Error: "Комментарий не может быть пустым"}, nil
	}

	seriesID := int64(request.Id)

	// Сохраняем комментарий в БД
	comment, err := s.Store.AddComment(ctx, &userID, &seriesID, request.Body.Body)
	if err != nil {
		log.Printf("AddComment: AddComment error: %v", err)
		return AddComment400JSONResponse{Error: "Не удалось добавить комментарий"}, nil
	}

	// Получаем имя пользователя
	user, err := s.Store.GetUserByID(ctx, userID)
	if err != nil {
		return AddComment400JSONResponse{Error: "Пользователь не найден"}, nil
	}

	return AddComment201JSONResponse{
		Id:        int(comment.ID),
		Username:  user.Username,
		Body:      comment.Body,
		CreatedAt: comment.CreatedAt,
	}, nil
}

// ==================== ЭНДПОЙНТЫ РЕЙТИНГА ====================

// GetSeriesRating обрабатывает GET /series/{id}/rating - получение среднего рейтинга сериала
// Рейтинг вычисляется через AVG() из таблицы ratings
// Не требует авторизации (любой может посмотреть рейтинг)
func (s *Server) GetSeriesRating(ctx context.Context, request GetSeriesRatingRequestObject) (GetSeriesRatingResponseObject, error) {
	log.Printf("GET /series/%d/rating", request.Id)

	seriesID := int64(request.Id)
	// Получаем среднюю оценку из БД (возвращает float64)
	avgFloat, err := s.Store.GetAverageRating(ctx, &seriesID)
	if err != nil {
		return GetSeriesRating404JSONResponse{Error: "Сериал не найден"}, nil
	}

	return GetSeriesRating200JSONResponse{
		SeriesId: request.Id,
		Average:  float32(avgFloat),
	}, nil
}

// RateSeries обрабатывает POST /series/{id}/rating - добавление или обновление оценки пользователя
// Требует авторизации (userID извлекается из контекста)
// Один пользователь может иметь только одну оценку на сериал (UPSERT через ON CONFLICT)
func (s *Server) RateSeries(ctx context.Context, request RateSeriesRequestObject) (RateSeriesResponseObject, error) {
	log.Printf("POST /series/%d/rating: оценка %d", request.Id, request.Body.Score)

	// Проверяем валидность оценки
	if request.Body.Score < 1 || request.Body.Score > 10 {
		return RateSeries400JSONResponse{Error: "Оценка должна быть от 1 до 10"}, nil
	}

	// Получаем userID из контекста
	userID, ok := GetUserIDFromContext(ctx)
	if !ok {
		log.Printf("RateSeries: userID not found in context")
		return RateSeries401JSONResponse{Error: "Требуется авторизация"}, nil
	}
	log.Printf("RateSeries: userID=%d", userID)

	seriesID := int64(request.Id)
	log.Printf("RateSeries: seriesID=%d", seriesID)

	// Конвертируем оценку в pgtype.Numeric
	ratingValue := pgtype.Numeric{}
	log.Printf("RateSeries: converting score %d to pgtype.Numeric", request.Body.Score)
	if err := ratingValue.Scan(fmt.Sprintf("%d", request.Body.Score)); err != nil {
		log.Printf("RateSeries: Scan error: %v", err)
		return RateSeries400JSONResponse{Error: "Неверный формат оценки"}, nil
	}
	log.Printf("RateSeries: ratingValue=%+v", ratingValue)

	// Сохраняем оценку
	_, err := s.Store.UpsertRating(ctx, &userID, &seriesID, ratingValue)
	if err != nil {
		log.Printf("RateSeries: UpsertRating error: %v", err)
		return RateSeries400JSONResponse{Error: "Не удалось сохранить оценку"}, nil
	}
	log.Printf("RateSeries: UpsertRating успешно")

	// Получаем средний рейтинг
	avg, err := s.Store.GetAverageRating(ctx, &seriesID)
	if err != nil {
		log.Printf("RateSeries: GetAverageRating error: %v", err)
		return RateSeries200JSONResponse{
			SeriesId: request.Id,
			Average:  0,
		}, nil
	}
	log.Printf("RateSeries: средний рейтинг = %f", avg)

	return RateSeries200JSONResponse{
		SeriesId: request.Id,
		Average:  float32(avg),
	}, nil
}
