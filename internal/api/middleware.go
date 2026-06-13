package api

import (
	"context"
	"net/http"
	"time"

	"github.com/pprAImm/database/store"
)

type contextKey string

const (
	UserIDKey    contextKey = "userID"
	SessionIDKey contextKey = "sessionID"
)

// AuthMiddleware извлекает userID и sessionID из сессионной cookie
func AuthMiddleware(store store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Получаем cookie
			cookie, err := r.Cookie("session_id")
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			sessionID := cookie.Value

			// Ищем сессию в БД
			session, err := store.GetSession(r.Context(), sessionID)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			// Проверяем, не истекла ли сессия
			if session.ExpiresAt.Before(time.Now()) {
				next.ServeHTTP(w, r)
				return
			}

			// Кладём userID и sessionID в контекст
			ctx := context.WithValue(r.Context(), UserIDKey, *session.UserID)
			ctx = context.WithValue(ctx, SessionIDKey, sessionID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserIDFromContext извлекает userID из контекста
func GetUserIDFromContext(ctx context.Context) (int64, bool) {
	userID, ok := ctx.Value(UserIDKey).(int64)
	return userID, ok
}

// GetSessionIDFromContext извлекает sessionID из контекста
func GetSessionIDFromContext(ctx context.Context) (string, bool) {
	sessionID, ok := ctx.Value(SessionIDKey).(string)
	return sessionID, ok
}
