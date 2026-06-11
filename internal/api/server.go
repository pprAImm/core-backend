package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"golang.org/x/crypto/bcrypt"

	"github.com/pprAImm/database/store"
)

// Server — структура, содержащая все обработчики API
type Server struct {
	Store store.Store
}

// NewServer создаёт новый экземпляр сервера
func NewServer(s store.Store) *Server {
	return &Server{Store: s}
}

// generateSessionID генерирует случайный токен для сессии
func generateSessionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// hashPassword хеширует пароль с помощью bcrypt
func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// checkPasswordHash проверяет, соответствует ли пароль хешу
func checkPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// getCurrentUserID получает ID текущего пользователя из контекста
// ВРЕМЕННАЯ ЗАГЛУШКА: пока возвращает 1
func getCurrentUserID() int64 {
	return 1
}

// ==================== ЭНДПОЙНТЫ АВТОРИЗАЦИИ ====================

// Register создаёт нового пользователя
func (s *Server) Register(ctx context.Context, request RegisterRequestObject) (RegisterResponseObject, error) {
	log.Printf("POST /auth/register: %s", request.Body.Email)

	hashedPassword, err := hashPassword(request.Body.Password)
	if err != nil {
		return Register409JSONResponse{Error: "Внутренняя ошибка"}, nil
	}

	user, err := s.Store.CreateUser(ctx, request.Body.Username, string(request.Body.Email), hashedPassword)
	if err != nil {
		return Register409JSONResponse{Error: "Пользователь с таким email уже существует"}, nil
	}

	return Register201JSONResponse{
		Email:    openapi_types.Email(user.Email),
		Id:       int(user.ID),
		Username: user.Username,
	}, nil
}

// Login аутентифицирует пользователя и создаёт сессию
func (s *Server) Login(ctx context.Context, request LoginRequestObject) (LoginResponseObject, error) {
	log.Printf("POST /auth/login: %s", request.Body.Email)

	user, err := s.Store.GetUserByEmail(ctx, string(request.Body.Email))
	if err != nil {
		return Login401JSONResponse{Error: "Неверный email или пароль"}, nil
	}

	if !checkPasswordHash(request.Body.Password, user.PasswordHash) {
		return Login401JSONResponse{Error: "Неверный email или пароль"}, nil
	}

	sessionID, err := generateSessionID()
	if err != nil {
		return Login401JSONResponse{Error: "Не удалось создать сессию"}, nil
	}

	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	userID := user.ID
	_, err = s.Store.CreateSession(ctx, sessionID, &userID, expiresAt)
	if err != nil {
		return Login401JSONResponse{Error: "Не удалось создать сессию"}, nil
	}

	setCookie := "session_id=" + sessionID + "; HttpOnly; Path=/; Expires=" + expiresAt.Format(time.RFC1123)

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

// Logout завершает текущую сессию
func (s *Server) Logout(ctx context.Context, request LogoutRequestObject) (LogoutResponseObject, error) {
	log.Println("POST /auth/logout")
	return Logout200Response{}, nil
}

// ==================== ЭНДПОЙНТЫ КАТЕГОРИЙ ====================

// GetAllCategories возвращает список всех категорий
func (s *Server) GetAllCategories(ctx context.Context, request GetAllCategoriesRequestObject) (GetAllCategoriesResponseObject, error) {
	log.Println("GET /categories")

	categories, err := s.Store.GetAllCategories(ctx)
	if err != nil {
		log.Printf("Ошибка получения категорий: %v", err)
		return GetAllCategories200JSONResponse{}, nil
	}

	// Конвертируем модели базы данных в формат API
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

// GetCategoryBySlug возвращает категорию с её сериалами
func (s *Server) GetCategoryBySlug(ctx context.Context, request GetCategoryBySlugRequestObject) (GetCategoryBySlugResponseObject, error) {
	log.Printf("GET /categories/%s", request.Slug)

	// Получаем категорию
	category, err := s.Store.GetCategoryBySlug(ctx, request.Slug)
	if err != nil {
		return GetCategoryBySlug404JSONResponse{Error: "Категория не найдена"}, nil
	}

	// Получаем сериалы в этой категории
	series, err := s.Store.GetSeriesByCategory(ctx, &category.ID)
	if err != nil {
		// При ошибке возвращаем категорию без сериалов
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

	// Формируем ответ
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

	// Конвертируем сериалы в формат API
	if len(series) > 0 {
		apiSeries := make([]struct {
			AverageRating *float32 `json:"average_rating,omitempty"`
			CategoryId    *int     `json:"category_id,omitempty"`
			CoverUrl      *string  `json:"cover_url,omitempty"`
			Description   *string  `json:"description,omitempty"`
			Id            int      `json:"id"`
			Title         string   `json:"title"`
		}, len(series))

		for i, s := range series {
			var avgRating *float32
			if s.Rating.Valid {
				f, _ := s.Rating.Float64Value()
				val := float32(f.Float64)
				avgRating = &val
			}

			apiSeries[i] = struct {
				AverageRating *float32 `json:"average_rating,omitempty"`
				CategoryId    *int     `json:"category_id,omitempty"`
				CoverUrl      *string  `json:"cover_url,omitempty"`
				Description   *string  `json:"description,omitempty"`
				Id            int      `json:"id"`
				Title         string   `json:"title"`
			}{
				Id:            int(s.ID),
				Title:         s.Title,
				Description:   s.Description,
				CoverUrl:      s.CoverUrl,
				CategoryId:    nil,
				AverageRating: avgRating,
			}
		}
		result.Series = &apiSeries
	}

	return result, nil
}

// ==================== ЭНДПОЙНТЫ СЕРИАЛОВ ====================

// GetSeriesById возвращает сериал с его эпизодами
func (s *Server) GetSeriesById(ctx context.Context, request GetSeriesByIdRequestObject) (GetSeriesByIdResponseObject, error) {
	log.Printf("GET /series/%d", request.Id)

	// Получаем сериал
	series, err := s.Store.GetSeriesByID(ctx, int64(request.Id))
	if err != nil {
		return GetSeriesById404JSONResponse{Error: "Сериал не найден"}, nil
	}

	// Получаем эпизоды
	episodes, err := s.Store.GetEpisodesBySeries(ctx, &series.ID)
	if err != nil {
		// При ошибке возвращаем сериал без эпизодов
		result := GetSeriesById200JSONResponse{
			Series: &struct {
				AverageRating *float32 `json:"average_rating,omitempty"`
				CategoryId    *int     `json:"category_id,omitempty"`
				CoverUrl      *string  `json:"cover_url,omitempty"`
				Description   *string  `json:"description,omitempty"`
				Id            int      `json:"id"`
				Title         string   `json:"title"`
			}{
				Id:          int(series.ID),
				Title:       series.Title,
				Description: series.Description,
				CoverUrl:    series.CoverUrl,
				CategoryId:  nil,
			},
		}
		return result, nil
	}

	// Формируем ответ
	result := GetSeriesById200JSONResponse{
		Series: &struct {
			AverageRating *float32 `json:"average_rating,omitempty"`
			CategoryId    *int     `json:"category_id,omitempty"`
			CoverUrl      *string  `json:"cover_url,omitempty"`
			Description   *string  `json:"description,omitempty"`
			Id            int      `json:"id"`
			Title         string   `json:"title"`
		}{
			Id:          int(series.ID),
			Title:       series.Title,
			Description: series.Description,
			CoverUrl:    series.CoverUrl,
			CategoryId:  nil,
		},
	}

	// Конвертируем эпизоды в формат API
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

// SearchSeries ищет сериалы по названию
func (s *Server) SearchSeries(ctx context.Context, request SearchSeriesRequestObject) (SearchSeriesResponseObject, error) {
	log.Printf("GET /series/search?q=%s", request.Params.Q)

	results, err := s.Store.SearchSeries(ctx, &request.Params.Q)
	if err != nil {
		log.Printf("Ошибка поиска: %v", err)
		return SearchSeries200JSONResponse{}, nil
	}

	// Конвертируем результаты в формат API
	apiResults := make(SearchSeries200JSONResponse, len(results))
	for i, r := range results {
		var avgRating *float32
		if r.Rating.Valid {
			f, _ := r.Rating.Float64Value()
			val := float32(f.Float64)
			avgRating = &val
		}

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
			AverageRating: avgRating,
		}
	}

	return apiResults, nil
}

// ==================== ЭНДПОЙНТЫ КОММЕНТАРИЕВ ====================

// GetSeriesComments возвращает все комментарии к сериалу
func (s *Server) GetSeriesComments(ctx context.Context, request GetSeriesCommentsRequestObject) (GetSeriesCommentsResponseObject, error) {
	log.Printf("GET /series/%d/comments", request.Id)

	seriesID := int64(request.Id)
	comments, err := s.Store.GetCommentsBySeries(ctx, &seriesID)
	if err != nil {
		return GetSeriesComments200JSONResponse{}, nil
	}

	// Конвертируем комментарии в формат API
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

// AddComment добавляет новый комментарий (требует авторизации)
func (s *Server) AddComment(ctx context.Context, request AddCommentRequestObject) (AddCommentResponseObject, error) {
	log.Printf("POST /series/%d/comments: %s", request.Id, request.Body.Body)

	// ВРЕМЕННАЯ ЗАГЛУШКА: ID пользователя = 1
	userID := getCurrentUserID()
	seriesID := int64(request.Id)

	comment, err := s.Store.AddComment(ctx, &userID, &seriesID, request.Body.Body)
	if err != nil {
		return AddComment400JSONResponse{Error: "Не удалось добавить комментарий"}, nil
	}

	return AddComment201JSONResponse{
		Id:        int(comment.ID),
		Username:  "current_user",
		Body:      comment.Body,
		CreatedAt: comment.CreatedAt,
	}, nil
}

// ==================== ЭНДПОЙНТЫ РЕЙТИНГА ====================

// GetSeriesRating возвращает среднюю оценку сериала
func (s *Server) GetSeriesRating(ctx context.Context, request GetSeriesRatingRequestObject) (GetSeriesRatingResponseObject, error) {
	log.Printf("GET /series/%d/rating", request.Id)

	seriesID := int64(request.Id)
	rating, err := s.Store.GetAverageRating(ctx, &seriesID)
	if err != nil {
		return GetSeriesRating404JSONResponse{Error: "Сериал не найден"}, nil
	}

	var avg float32
	if rating.Valid {
		f, _ := rating.Float64Value()
		avg = float32(f.Float64)
	}

	return GetSeriesRating200JSONResponse{
		SeriesId: request.Id,
		Average:  avg,
	}, nil
}

// RateSeries добавляет или обновляет оценку пользователя для сериала
func (s *Server) RateSeries(ctx context.Context, request RateSeriesRequestObject) (RateSeriesResponseObject, error) {
	log.Printf("POST /series/%d/rating: оценка %d", request.Id, request.Body.Score)

	// ВРЕМЕННАЯ ЗАГЛУШКА: ID пользователя = 1
	userID := getCurrentUserID()
	seriesID := int64(request.Id)
	score := int32(request.Body.Score)

	// Сохраняем или обновляем оценку
	_, err := s.Store.UpsertRating(ctx, &userID, &seriesID, &score)
	if err != nil {
		return RateSeries400JSONResponse{Error: "Оценка должна быть от 1 до 10"}, nil
	}

	// Получаем обновлённый средний рейтинг
	rating, err := s.Store.GetAverageRating(ctx, &seriesID)
	if err != nil {
		return RateSeries200JSONResponse{
			SeriesId: request.Id,
			Average:  0,
		}, nil
	}

	var avg float32
	if rating.Valid {
		f, _ := rating.Float64Value()
		avg = float32(f.Float64)
	}

	return RateSeries200JSONResponse{
		SeriesId: request.Id,
		Average:  avg,
	}, nil
}
