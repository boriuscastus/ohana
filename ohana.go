package ohana

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Client представляет клиент для работы с BotFather
type Client struct {
	config *Config
}

// NewClient создает нового клиента
func NewClient(config *Config) *Client {
	if config.SessionPath == "" {
		config.SessionPath = "telegram_session.json"
	}

	return &Client{config: config}
}

// ========== ОСНОВНЫЕ ФУНКЦИИ ==========

// CreateBot создает нового бота и возвращает токен
func (c *Client) CreateBot(ctx context.Context, name, username string) (string, error) {
	token, err := c.createBot(ctx, name, username, "")
	if err != nil {
		// Проверяем специфичные ошибки
		errStr := err.Error()
		if strings.Contains(errStr, ErrUsernameTaken) {
			return "", fmt.Errorf("username '@%s' уже занят", username)
		}
		if strings.Contains(errStr, ErrTooManyAttempts) ||
			strings.Contains(errStr, ErrRateLimited) {
			return "", fmt.Errorf("слишком много попыток, подождите: %v", err)
		}
		if strings.Contains(errStr, ErrInvalidUsername) {
			return "", fmt.Errorf("неверный формат username. Должен оканчиваться на 'bot'")
		}
	}
	return token, err
}

// CreateBotWithDescription создает бота с описанием
func (c *Client) CreateBotWithDescription(ctx context.Context, name, username, description string) (string, error) {
	return c.createBot(ctx, name, username, description)
}

// CreateBotWithRetry создает бота с автоматическим запросом username
func (c *Client) CreateBotWithRetry(ctx context.Context, name string, askUsername func() string) (username, token string, err error) {
	maxAttempts := 5
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		username = askUsername()
		if username == "" {
			return "", "", fmt.Errorf("username не может быть пустым")
		}

		if !strings.HasSuffix(strings.ToLower(username), "bot") {
			return "", "", fmt.Errorf("username должен оканчиваться на 'bot'")
		}

		token, err = c.CreateBot(ctx, name, username)
		if err != nil {
			if strings.Contains(err.Error(), ErrUsernameTaken) {
				fmt.Printf("❌ Username '@%s' занят (попытка %d/%d)\n", username, attempt, maxAttempts)
				if attempt < maxAttempts {
					fmt.Println("🔁 Пробуем другой username...")
					continue
				} else {
					return "", "", fmt.Errorf("не удалось найти свободный username после %d попыток", maxAttempts)
				}
			}
			if strings.Contains(err.Error(), ErrTooManyAttempts) {
				return "", "", fmt.Errorf("слишком много попыток создания бота, подождите")
			}
			return "", "", err
		}

		return username, token, nil
	}

	return "", "", fmt.Errorf("не удалось создать бота")
}

// ========== ФУНКЦИИ НАСТРОЙКИ БОТА ==========

// SetBotName изменяет имя бота
func (c *Client) SetBotName(ctx context.Context, botUsername, newName string) error {
	return c.execWithBotFather(ctx, botUsername, "/setname", newName,
		[]string{"send me the new name", "choose a name", "what name"},
		[]string{"success", "updated", "done", "name updated"})
}

// SetBotDescription изменяет описание бота
func (c *Client) SetBotDescription(ctx context.Context, botUsername, description string) error {
	return c.execWithBotFather(ctx, botUsername, "/setdescription", description,
		[]string{"send me the new description", "what description", "description for the bot"},
		[]string{"success", "updated", "done", "description updated"})
}

// SetBotAbout изменяет информацию "О боте"
func (c *Client) SetBotAbout(ctx context.Context, botUsername, aboutText string) error {
	return c.execWithBotFather(ctx, botUsername, "/setabouttext", aboutText,
		[]string{"about", "send me", "new text", "about text"},
		[]string{"success", "updated", "done", "about section updated"})
}

// SetBotCommands устанавливает команды бота
func (c *Client) SetBotCommands(ctx context.Context, botUsername string, commands map[string]string) error {
	// Форматируем команды согласно требованиям BotFather
	commandsText := FormatCommands(commands)

	// Проверяем, что команды в правильном формате
	if !validateCommandsFormat(commandsText) {
		return fmt.Errorf("неверный формат команд. Используйте: команда - описание")
	}

	return c.execWithBotFather(ctx, botUsername, "/setcommands", commandsText,
		[]string{"send me a list of commands", "list of commands", "command1 - description"},
		[]string{"success", "updated", "done", "command list updated"})
}

// SetBotUserpic устанавливает фото профиля бота
func (c *Client) SetBotUserpic(ctx context.Context, botUsername, imagePath string) error {
	client, err := c.ensureSession(ctx)
	if err != nil {
		return err
	}

	return client.Run(ctx, func(ctx context.Context) error {
		api := client.API()

		botFather, err := findBotFather(ctx, api)
		if err != nil {
			return err
		}

		// 1. Отправляем /setuserpic
		if err := sendMessageWithRetry(ctx, api, botFather, "/setuserpic", 3); err != nil {
			return err
		}

		// 2. Ждем выбор бота
		resp, err := waitForResponseWithChecks(ctx, api, botFather,
			[]string{"choose a bot", "select a bot", "which bot"},
			30*time.Second)
		if err != nil {
			return fmt.Errorf("ожидание выбора бота: %w", err)
		}

		// Проверяем, что бот существует
		if strings.Contains(strings.ToLower(resp), "not found") ||
			strings.Contains(strings.ToLower(resp), "no bot") {
			return fmt.Errorf("бот @%s не найден", botUsername)
		}

		// 3. Отправляем username бота с @
		botUsernameWithAt := "@" + botUsername
		if err := sendMessageWithRetry(ctx, api, botFather, botUsernameWithAt, 3); err != nil {
			return fmt.Errorf("не удалось отправить username бота: %w", err)
		}

		// 4. Ждем запрос фото
		if _, err := waitForResponseWithChecks(ctx, api, botFather,
			[]string{"send me the new profile photo", "profile photo", "photo for the bot", "ok. send me"},
			30*time.Second); err != nil {
			return fmt.Errorf("ожидание запроса фото: %w", err)
		}

		// 5. Отправляем фото
		if err := sendPhoto(ctx, api, botFather, imagePath); err != nil {
			return err
		}

		// 6. Ждем подтверждение
		if _, err := waitForResponseWithChecks(ctx, api, botFather,
			[]string{"success", "updated", "done", "photo updated"},
			30*time.Second); err != nil {
			return fmt.Errorf("ожидание подтверждения: %w", err)
		}

		return nil
	})
}

// DeleteBot удаляет бота
func (c *Client) DeleteBot(ctx context.Context, botUsername string) error {
	return c.execWithBotFather(ctx, botUsername, "/deletebot", "Yes, I am totally sure.",
		[]string{"are you sure", "confirm", "delete this bot", "yes, i am totally sure"},
		[]string{"deleted", "successfully deleted", "bot has been deleted", "done", "bot is gone"})
}

// ========== ВНУТРЕННИЕ ФУНКЦИИ ==========

// validateCommandsFormat проверяет формат команд
func validateCommandsFormat(commandsText string) bool {
	lines := strings.Split(commandsText, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Проверяем формат: команда - описание
		if !strings.Contains(line, " - ") {
			return false
		}
	}
	return true
}

// createBot создает бота
func (c *Client) createBot(ctx context.Context, name, username, description string) (string, error) {
	client, err := c.ensureSession(ctx)
	if err != nil {
		return "", err
	}

	var token string

	err = client.Run(ctx, func(ctx context.Context) error {
		api := client.API()

		botFather, err := findBotFather(ctx, api)
		if err != nil {
			return err
		}

		// 1. Отправляем /newbot
		if err := sendMessageWithRetry(ctx, api, botFather, "/newbot", 3); err != nil {
			return err
		}

		// 2. Ждем запрос имени
		resp, err := waitForResponseWithChecks(ctx, api, botFather,
			[]string{"choose a name", "how are we going to call", "alright, a new bot", "good. now let's choose"},
			30*time.Second)
		if err != nil {
			return fmt.Errorf("ожидание запроса имени: %w", err)
		}

		// 3. Отправляем имя бота
		if err := sendMessageWithRetry(ctx, api, botFather, name, 3); err != nil {
			return err
		}

		// 4. Ждем запрос username
		resp, err = waitForResponseWithChecks(ctx, api, botFather,
			[]string{"choose a username", "username for your bot", "good. now let's choose"},
			30*time.Second)
		if err != nil {
			return fmt.Errorf("ожидание запроса username: %w", err)
		}

		// 5. Отправляем username
		if err := sendMessageWithRetry(ctx, api, botFather, username, 3); err != nil {
			return err
		}

		// 6. Ждем ответ (может быть токен или ошибка)
		resp, err = waitForResponseWithChecks(ctx, api, botFather,
			[]string{"done", "congratulations", "use this token", "sorry", "invalid", "already taken"},
			30*time.Second)
		if err != nil {
			return err
		}

		// Проверяем на ошибки
		if err := CheckBotFatherError(resp); err != nil {
			return err
		}

		// Извлекаем токен
		token = ParseToken(resp)
		if token == "" {
			// Если токена нет, ждем еще немного
			resp, err = waitForResponseWithChecks(ctx, api, botFather,
				[]string{"done", "congratulations", "use this token"},
				10*time.Second)
			if err != nil {
				return fmt.Errorf("не удалось получить токен: %w", err)
			}
			token = ParseToken(resp)
			if token == "" {
				return fmt.Errorf("не удалось извлечь токен из ответа BotFather")
			}
		}

		// 7. Устанавливаем описание (если указано)
		if description != "" {
			if err := c.execWithBotFather(ctx, username, "/setdescription", description,
				[]string{"send me the new description", "what description"},
				[]string{"success", "updated", "done"}); err != nil {
				fmt.Printf("⚠️  Не удалось установить описание: %v\n", err)
			}
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	return token, nil
}

// execWithBotFather выполняет команду с BotFather
func (c *Client) execWithBotFather(ctx context.Context, botUsername, command, text string,
	waitKeywords, successKeywords []string) error {

	client, err := c.ensureSession(ctx)
	if err != nil {
		return err
	}

	return client.Run(ctx, func(ctx context.Context) error {
		api := client.API()

		botFather, err := findBotFather(ctx, api)
		if err != nil {
			return err
		}

		// 1. Отправляем команду
		if err := sendMessageWithRetry(ctx, api, botFather, command, 3); err != nil {
			return err
		}

		// 2. Ждем выбор бота
		resp, err := waitForResponseWithChecks(ctx, api, botFather,
			[]string{"choose a bot", "select a bot", "which bot"},
			30*time.Second)
		if err != nil {
			return fmt.Errorf("ожидание выбора бота: %w", err)
		}

		// Проверяем, что бот существует
		if strings.Contains(strings.ToLower(resp), "not found") ||
			strings.Contains(strings.ToLower(resp), "no bot") ||
			strings.Contains(strings.ToLower(resp), "invalid") {
			return fmt.Errorf("бот @%s не найден или невалиден", botUsername)
		}

		// 3. Отправляем username бота с @
		botUsernameWithAt := "@" + botUsername
		if err := sendMessageWithRetry(ctx, api, botFather, botUsernameWithAt, 3); err != nil {
			return fmt.Errorf("не удалось отправить username бота: %w", err)
		}

		// 4. Ждем запрос
		if _, err := waitForResponseWithChecks(ctx, api, botFather, waitKeywords, 30*time.Second); err != nil {
			return fmt.Errorf("ожидание запроса: %w", err)
		}

		// 5. Отправляем текст
		if err := sendMessageWithRetry(ctx, api, botFather, text, 3); err != nil {
			return fmt.Errorf("не удалось отправить текст: %w", err)
		}

		// 6. Ждем подтверждение
		if _, err := waitForResponseWithChecks(ctx, api, botFather, successKeywords, 30*time.Second); err != nil {
			return fmt.Errorf("ожидание подтверждения: %w", err)
		}

		return nil
	})
}
