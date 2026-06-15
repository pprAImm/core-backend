.PHONY: docker-build docker-save docker-load docker-run docker-stop docker-restart docker-logs deploy

# Переменные для Docker
DOCKER_IMAGE_NAME := core-backend
DOCKER_IMAGE_TAG := latest
DOCKER_IMAGE := $(DOCKER_IMAGE_NAME):$(DOCKER_IMAGE_TAG)
DOCKER_TAR := $(DOCKER_IMAGE_NAME).tar
SERVER_HOST := 185.23.34.25
SERVER_USER := root
SERVER_PATH := /root

# Сборка Docker образа
docker-build: ## Build Docker image locally
	@echo "Building Docker image $(DOCKER_IMAGE)..."
	docker build -t $(DOCKER_IMAGE) .
	@echo "Docker image built successfully"

# Сохранение образа в tar файл
docker-save: docker-build ## Save Docker image to tar file
	@echo "Saving Docker image to $(DOCKER_TAR)..."
	docker save $(DOCKER_IMAGE) -o $(DOCKER_TAR)
	@ls -lh $(DOCKER_TAR)

# Копирование на сервер
docker-copy: docker-save ## Copy Docker image to server
	@echo "Copying $(DOCKER_TAR) to $(SERVER_HOST)..."
	scp $(DOCKER_TAR) $(SERVER_USER)@$(SERVER_HOST):$(SERVER_PATH)/$(DOCKER_TAR)

# Загрузка образа на сервере
docker-load: ## Load Docker image on server
	@echo "Loading Docker image on server..."
	ssh $(SERVER_USER)@$(SERVER_HOST) "docker load -i $(SERVER_PATH)/$(DOCKER_TAR)"

# Остановка и удаление старого контейнера
docker-remove: ## Stop and remove existing container on server
	@echo "Stopping and removing old container..."
	-ssh $(SERVER_USER)@$(SERVER_HOST) "docker stop $(DOCKER_IMAGE_NAME) || true"
	-ssh $(SERVER_USER)@$(SERVER_HOST) "docker rm $(DOCKER_IMAGE_NAME) || true"

# Запуск контейнера на сервере
docker-run: docker-remove docker-load ## Run container on server
	@echo "Starting container on server..."
	ssh $(SERVER_USER)@$(SERVER_HOST) "docker run -d \
		--name $(DOCKER_IMAGE_NAME) \
		--restart always \
		-p 8080:8080 \
		--env-file $(SERVER_PATH)/.env \
		$(DOCKER_IMAGE)"

# Полный деплой (сборка, копирование, запуск)
deploy: docker-copy docker-run ## Full deploy: build, copy and run on server
	@echo "Deployment completed!"
	@echo "API available at http://$(SERVER_HOST):8080"
	@echo "Check logs with: make docker-logs"

# Проверка статуса контейнера на сервере
docker-status: ## Show container status on server
	@ssh $(SERVER_USER)@$(SERVER_HOST) "docker ps --filter name=$(DOCKER_IMAGE_NAME)"

# Просмотр логов
docker-logs: ## Show container logs
	@ssh $(SERVER_USER)@$(SERVER_HOST) "docker logs --tail 50 $(DOCKER_IMAGE_NAME)"

# Просмотр логов в реальном времени
docker-logs-follow: ## Follow container logs
	@ssh $(SERVER_USER)@$(SERVER_HOST) "docker logs -f $(DOCKER_IMAGE_NAME)"

# Перезапуск контейнера
docker-restart: ## Restart container on server
	@ssh $(SERVER_USER)@$(SERVER_HOST) "docker restart $(DOCKER_IMAGE_NAME)"

# Остановка контейнера
docker-stop: ## Stop container on server
	@ssh $(SERVER_USER)@$(SERVER_HOST) "docker stop $(DOCKER_IMAGE_NAME)"

# Очистка старых образов на сервере
docker-clean: ## Remove old docker images on server
	@ssh $(SERVER_USER)@$(SERVER_HOST) "docker image prune -f"

# Быстрое обновление (без полной пересборки, если образ уже есть)
quick-deploy: docker-copy docker-run ## Quick deploy (skip build if image exists)
	@echo "Quick deployment completed!"
