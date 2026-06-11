package api

import (
	"context"
	"log"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Server — реализация StrictServerInterface
type Server struct {
	// Здесь будут зависимости (например, store.Store)
}

// NewServer создаёт новый экземпляр сервера
func NewServer() *Server {
	return &Server{}
}

// ==================== АВТОРИЗАЦИЯ ====================

// Login — POST /auth/login
func (s *Server) Login(ctx context.Context, request LoginRequestObject) (LoginResponseObject, error) {
	log.Printf("📋 POST /auth/login: %s", request.Body.Email)

	// TODO: проверить пароль и создать сессию
	// Пока заглушка
	setCookie := "session_id=abc123; HttpOnly; Path=/"

	return Login200JSONResponse{
		Body: struct {
			Email    openapi_types.Email `json:"email"`
			Id       int                 `json:"id"`
			Username string              `json:"username"`
		}{
			Email:    request.Body.Email,
			Id:       1,
			Username: "testuser",
		},
		Headers: Login200ResponseHeaders{
			SetCookie: &setCookie,
		},
	}, nil
}

// Logout — POST /auth/logout
func (s *Server) Logout(ctx context.Context, request LogoutRequestObject) (LogoutResponseObject, error) {
	log.Println("📋 POST /auth/logout")

	// TODO: удалить сессию
	return Logout200Response{}, nil
}

// Register — POST /auth/register
func (s *Server) Register(ctx context.Context, request RegisterRequestObject) (RegisterResponseObject, error) {
	log.Printf("📋 POST /auth/register: %s, %s", request.Body.Email, request.Body.Username)

	// TODO: создать пользователя в БД
	return Register201JSONResponse{
		Email:    request.Body.Email,
		Id:       1,
		Username: request.Body.Username,
	}, nil
}

// ==================== КАТЕГОРИИ ====================

// GetAllCategories — GET /categories
func (s *Server) GetAllCategories(ctx context.Context, request GetAllCategoriesRequestObject) (GetAllCategoriesResponseObject, error) {
	log.Println("📋 GET /categories")

	// TODO: получить из БД
	categories := GetAllCategories200JSONResponse{
		{Id: 1, Name: "Аниме", Slug: "anime"},
		{Id: 2, Name: "Фэнтези", Slug: "fantasy"},
		{Id: 3, Name: "Научная фантастика", Slug: "sci-fi"},
	}

	return categories, nil
}

// GetCategoryBySlug — GET /categories/{slug}
func (s *Server) GetCategoryBySlug(ctx context.Context, request GetCategoryBySlugRequestObject) (GetCategoryBySlugResponseObject, error) {
	log.Printf("📋 GET /categories/%s", request.Slug)

	// TODO: получить из БД
	category := struct {
		Id   int    `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	}{
		Id:   1,
		Name: "Аниме",
		Slug: "anime",
	}

	series := []struct {
		AverageRating *float32 `json:"average_rating,omitempty"`
		CategoryId    *int     `json:"category_id,omitempty"`
		CoverUrl      *string  `json:"cover_url,omitempty"`
		Description   *string  `json:"description,omitempty"`
		Id            int      `json:"id"`
		Title         string   `json:"title"`
	}{
		{
			Id:            1,
			Title:         "Атака Титанов",
			Description:   strPtr("Эпическое аниме"),
			CategoryId:    intPtr(1),
			AverageRating: float32Ptr(8.9),
		},
	}

	return GetCategoryBySlug200JSONResponse{
		Category: &category,
		Series:   &series,
	}, nil
}

// ==================== СЕРИАЛЫ ====================

// GetSeriesById — GET /series/{id}
func (s *Server) GetSeriesById(ctx context.Context, request GetSeriesByIdRequestObject) (GetSeriesByIdResponseObject, error) {
	log.Printf("📋 GET /series/%d", request.Id)

	// TODO: получить из БД
	series := &struct {
		AverageRating *float32 `json:"average_rating,omitempty"`
		CategoryId    *int     `json:"category_id,omitempty"`
		CoverUrl      *string  `json:"cover_url,omitempty"`
		Description   *string  `json:"description,omitempty"`
		Id            int      `json:"id"`
		Title         string   `json:"title"`
	}{
		Id:            int(request.Id),
		Title:         "Атака Титанов",
		Description:   strPtr("Эпическое аниме"),
		CategoryId:    intPtr(1),
		AverageRating: float32Ptr(8.9),
	}

	episodes := []struct {
		EpisodeNum *int    `json:"episode_num,omitempty"`
		Id         int     `json:"id"`
		SeriesId   int     `json:"series_id"`
		TiktokUrl  string  `json:"tiktok_url"`
		Title      *string `json:"title,omitempty"`
	}{
		{
			Id:         1,
			SeriesId:   int(request.Id),
			EpisodeNum: intPtr(1),
			Title:      strPtr("Пилот"),
			TiktokUrl:  "https://tiktok.com/@example/video/1",
		},
		{
			Id:         2,
			SeriesId:   int(request.Id),
			EpisodeNum: intPtr(2),
			Title:      strPtr("Разрушение"),
			TiktokUrl:  "https://tiktok.com/@example/video/2",
		},
	}

	return GetSeriesById200JSONResponse{
		Series:   series,
		Episodes: &episodes,
	}, nil
}

// SearchSeries — GET /series/search
func (s *Server) SearchSeries(ctx context.Context, request SearchSeriesRequestObject) (SearchSeriesResponseObject, error) {
	log.Printf("📋 GET /series/search?q=%s", request.Params.Q)

	// TODO: поиск в БД
	results := SearchSeries200JSONResponse{
		{
			Id:          1,
			Title:       "Атака Титанов",
			Description: strPtr("Эпическое аниме"),
			CategoryId:  intPtr(1),
		},
	}

	return results, nil
}

// ==================== КОММЕНТАРИИ ====================

// GetSeriesComments — GET /series/{id}/comments
func (s *Server) GetSeriesComments(ctx context.Context, request GetSeriesCommentsRequestObject) (GetSeriesCommentsResponseObject, error) {
	log.Printf("📋 GET /series/%d/comments", request.Id)

	// TODO: получить из БД
	comments := GetSeriesComments200JSONResponse{
		{
			Id:        1,
			Username:  "arina",
			Body:      "Отличный сериал!",
			CreatedAt: time.Now(),
		},
		{
			Id:        2,
			Username:  "ivan",
			Body:      "Жду следующую серию",
			CreatedAt: time.Now(),
		},
	}

	return comments, nil
}

// AddComment — POST /series/{id}/comments
func (s *Server) AddComment(ctx context.Context, request AddCommentRequestObject) (AddCommentResponseObject, error) {
	log.Printf("📋 POST /series/%d/comments: %s", request.Id, request.Body.Body)

	// TODO: сохранить в БД
	return AddComment201JSONResponse{
		Id:        3,
		Username:  "current_user",
		Body:      request.Body.Body,
		CreatedAt: time.Now(),
	}, nil
}

// ==================== РЕЙТИНГ ====================

// GetSeriesRating — GET /series/{id}/rating
func (s *Server) GetSeriesRating(ctx context.Context, request GetSeriesRatingRequestObject) (GetSeriesRatingResponseObject, error) {
	log.Printf("📋 GET /series/%d/rating", request.Id)

	// TODO: получить среднюю оценку из БД
	return GetSeriesRating200JSONResponse{
		SeriesId: int(request.Id),
		Average:  8.7,
	}, nil
}

// RateSeries — POST /series/{id}/rating
func (s *Server) RateSeries(ctx context.Context, request RateSeriesRequestObject) (RateSeriesResponseObject, error) {
	log.Printf("📋 POST /series/%d/rating: оценка %d", request.Id, request.Body.Score)

	// TODO: сохранить оценку в БД
	return RateSeries200JSONResponse{
		SeriesId: int(request.Id),
		Average:  8.5,
	}, nil
}

// ==================== ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ ====================

func strPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}

func float32Ptr(f float32) *float32 {
	return &f
}
