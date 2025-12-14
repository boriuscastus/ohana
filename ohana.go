package ohana

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/boriuscastus/ohana/mahalo"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

// Глобальный клиент
var ohanaClient *Client

func SetupConfig(apiID int, apiHash, phone, sessionPath string) {
	config := &Config{
		APIID:       apiID,
		APIHash:     apiHash,
		Phone:       phone,
		SessionPath: sessionPath,
	}

	if config.SessionPath == "" {
		config.SessionPath = "telegram_session.json"
	}

	ohanaClient = &Client{config: config}
}

// ========== ОСНОВНЫЕ ФУНКЦИИ ==========

// CreateBot создает нового бота и возвращает токен
func CreateBot(name, username string) (string, error) {
	ctx := context.Background()
	token, err := ohanaClient.createBot(ctx, name, username, "")
	if err != nil {
		// Проверяем специфичные ошибки
		errStr := err.Error()
		if strings.Contains(errStr, mahalo.ErrUsernameTaken) {
			return "", fmt.Errorf("username '@%s' уже занят", username)
		}
		if strings.Contains(errStr, mahalo.ErrTooManyAttempts) ||
			strings.Contains(errStr, mahalo.ErrRateLimited) {
			return "", fmt.Errorf("слишком много попыток, подождите: %v", err)
		}
		if strings.Contains(errStr, mahalo.ErrInvalidUsername) {
			return "", fmt.Errorf("неверный формат username. Должен оканчиваться на 'bot'")
		}
	}
	return token, err
}

// CreateBotWithDescription создает бота с описанием
func CreateBotWithDescription(name, username, description string) (string, error) {
	ctx := context.Background()
	token, err := ohanaClient.createBot(ctx, name, username, description)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, mahalo.ErrUsernameTaken) {
			return "", fmt.Errorf("username '@%s' уже занят", username)
		}
		if strings.Contains(errStr, mahalo.ErrTooManyAttempts) ||
			strings.Contains(errStr, mahalo.ErrRateLimited) {
			return "", fmt.Errorf("слишком много попыток, подождите: %v", err)
		}
		if strings.Contains(errStr, mahalo.ErrInvalidUsername) {
			return "", fmt.Errorf("неверный формат username. Должен оканчиваться на 'bot'")
		}
	}
	return token, err
}

// CreateBotWithRetry создает бота с автоматическим запросом username
func CreateBotWithRetry(name string, askUsername func() string) (username, token string, err error) {
	ctx := context.Background()
	return ohanaClient.createBotWithRetry(ctx, name, askUsername)
}

// ========== ФУНКЦИИ НАСТРОЙКИ БОТА ==========

// SetBotName изменяет имя бота
func SetBotName(botUsername, newName string) error {
	ctx := context.Background()
	return ohanaClient.execWithBotFather(ctx, botUsername, "/setname", newName,
		[]string{"send me the new name", "choose a name", "what name"},
		[]string{"success", "updated", "done", "name updated"})
}

// SetBotDescription изменяет описание бота
func SetBotDescription(botUsername, description string) error {
	ctx := context.Background()
	return ohanaClient.execWithBotFather(ctx, botUsername, "/setdescription", description,
		[]string{"send me the new description", "what description", "description for the bot"},
		[]string{"success", "updated", "done", "description updated"})
}

// SetBotAbout изменяет информацию "О боте"
func SetBotAbout(botUsername, aboutText string) error {
	ctx := context.Background()
	return ohanaClient.execWithBotFather(ctx, botUsername, "/setabouttext", aboutText,
		[]string{"about", "send me", "new text", "about text"},
		[]string{"success", "updated", "done", "about section updated"})
}

// SetBotCommands устанавливает команды бота
func SetBotCommands(botUsername string, commands map[string]string) error {
	ctx := context.Background()
	// Форматируем команды согласно требованиям BotFather
	commandsText := mahalo.FormatCommands(commands)

	// Проверяем, что команды в правильном формате
	if !validateCommandsFormat(commandsText) {
		return fmt.Errorf("неверный формат команд. Используйте: команда - описание")
	}

	return ohanaClient.execWithBotFather(ctx, botUsername, "/setcommands", commandsText,
		[]string{"send me a list of commands", "list of commands", "command1 - description"},
		[]string{"success", "updated", "done", "command list updated"})
}

// SetBotUserpic устанавливает фото профиля бота
func SetBotUserpic(botUsername, imagePath string) error {
	ctx := context.Background()
	return ohanaClient.setBotUserpic(ctx, botUsername, imagePath)
}

// DeleteBot удаляет бота
func DeleteBot(botUsername string) error {
	ctx := context.Background()
	return ohanaClient.execWithBotFather(ctx, botUsername, "/deletebot", "Yes, I am totally sure.",
		[]string{"are you sure", "confirm", "delete this bot", "yes, i am totally sure"},
		[]string{"deleted", "successfully deleted", "bot has been deleted", "done", "bot is gone"})
}

// ========== ВНУТРЕННИЕ СТРУКТУРЫ ==========

// Config содержит конфигурацию для подключения к Telegram
type Config struct {
	APIID       int
	APIHash     string
	Phone       string
	SessionPath string // опционально, по умолчанию "telegram_session.json"
}

// Client представляет клиент для работы с BotFather
type Client struct {
	config *Config
}

// createBot создает бота
func (c *Client) createBot(ctx context.Context, name, username, description string) (string, error) {
	tgClient, err := c.EnsureSession(ctx)
	if err != nil {
		return "", err
	}

	var token string

	err = tgClient.Run(ctx, func(ctx context.Context) error {
		api := tgClient.API()

		botFather, err := mahalo.FindBotFather(ctx, api)
		if err != nil {
			return err
		}

		// 1. Отправляем /newbot
		if err := mahalo.SendMessageWithRetry(ctx, api, botFather, "/newbot", 3); err != nil {
			return err
		}

		// 2. Ждем запрос имени
		resp, err := mahalo.WaitForResponseWithChecks(ctx, api, botFather,
			[]string{"choose a name", "how are we going to call", "alright, a new bot", "good. now let's choose"},
			30*time.Second)
		if err != nil {
			return fmt.Errorf("ожидание запроса имени: %w", err)
		}

		// 3. Отправляем имя бота
		if err := mahalo.SendMessageWithRetry(ctx, api, botFather, name, 3); err != nil {
			return err
		}

		// 4. Ждем запрос username
		resp, err = mahalo.WaitForResponseWithChecks(ctx, api, botFather,
			[]string{"choose a username", "username for your bot", "good. now let's choose"},
			30*time.Second)
		if err != nil {
			return fmt.Errorf("ожидание запроса username: %w", err)
		}

		// 5. Отправляем username
		if err := mahalo.SendMessageWithRetry(ctx, api, botFather, username, 3); err != nil {
			return err
		}

		// 6. Ждем ответ (может быть токен или ошибка)
		resp, err = mahalo.WaitForResponseWithChecks(ctx, api, botFather,
			[]string{"done", "congratulations", "use this token", "sorry", "invalid", "already taken"},
			30*time.Second)
		if err != nil {
			return err
		}

		// Проверяем на ошибки
		if err := mahalo.CheckBotFatherError(resp); err != nil {
			return err
		}

		// Извлекаем токен
		token = mahalo.ParseToken(resp)
		if token == "" {
			// Если токена нет, ждем еще немного
			resp, err = mahalo.WaitForResponseWithChecks(ctx, api, botFather,
				[]string{"done", "congratulations", "use this token"},
				10*time.Second)
			if err != nil {
				return fmt.Errorf("не удалось получить токен: %w", err)
			}
			token = mahalo.ParseToken(resp)
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

	return token, err
}

// createBotWithRetry создает бота с повторными попытками
func (c *Client) createBotWithRetry(ctx context.Context, name string, askUsername func() string) (username, token string, err error) {
	maxAttempts := 5
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		username = askUsername()
		if username == "" {
			return "", "", fmt.Errorf("username не может быть пустым")
		}

		if !strings.HasSuffix(strings.ToLower(username), "bot") {
			return "", "", fmt.Errorf("username должен оканчиваться на 'bot'")
		}

		// Вызываем createBot (не CreateBot!), чтобы получить сырые ошибки
		token, err = c.createBot(ctx, name, username, "")
		if err != nil {
			if strings.Contains(err.Error(), mahalo.ErrUsernameTaken) {
				fmt.Printf("❌ Username '@%s' занят (попытка %d/%d)\n", username, attempt, maxAttempts)
				if attempt < maxAttempts {
					fmt.Println("🔁 Пробуем другой username...")
					continue
				} else {
					return "", "", fmt.Errorf("не удалось найти свободный username после %d попыток", maxAttempts)
				}
			}
			if strings.Contains(err.Error(), mahalo.ErrTooManyAttempts) {
				return "", "", fmt.Errorf("слишком много попыток создания бота, подождите")
			}
			return "", "", err
		}

		return username, token, nil
	}

	return "", "", fmt.Errorf("не удалось создать бота")
}

// execWithBotFather выполняет команду с BotFather
func (c *Client) execWithBotFather(ctx context.Context, botUsername, command, text string,
	waitKeywords, successKeywords []string) error {

	tgClient, err := c.EnsureSession(ctx)
	if err != nil {
		return err
	}

	return tgClient.Run(ctx, func(ctx context.Context) error {
		api := tgClient.API()

		botFather, err := mahalo.FindBotFather(ctx, api)
		if err != nil {
			return err
		}

		// 1. Отправляем команду
		if err := mahalo.SendMessageWithRetry(ctx, api, botFather, command, 3); err != nil {
			return err
		}

		// 2. Ждем выбор бота
		resp, err := mahalo.WaitForResponseWithChecks(ctx, api, botFather,
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
		if err := mahalo.SendMessageWithRetry(ctx, api, botFather, botUsernameWithAt, 3); err != nil {
			return fmt.Errorf("не удалось отправить username бота: %w", err)
		}

		// 4. Ждем запрос
		if _, err := mahalo.WaitForResponseWithChecks(ctx, api, botFather, waitKeywords, 30*time.Second); err != nil {
			return fmt.Errorf("ожидание запроса: %w", err)
		}

		// 5. Отправляем текст
		if err := mahalo.SendMessageWithRetry(ctx, api, botFather, text, 3); err != nil {
			return fmt.Errorf("не удалось отправить текст: %w", err)
		}

		// 6. Ждем подтверждение
		if _, err := mahalo.WaitForResponseWithChecks(ctx, api, botFather, successKeywords, 30*time.Second); err != nil {
			return fmt.Errorf("ожидание подтверждения: %w", err)
		}

		return nil
	})
}

// setBotUserpic устанавливает фото профиля бота
func (c *Client) setBotUserpic(ctx context.Context, botUsername, imagePath string) error {
	tgClient, err := c.EnsureSession(ctx)
	if err != nil {
		return err
	}

	return tgClient.Run(ctx, func(ctx context.Context) error {
		api := tgClient.API()

		botFather, err := mahalo.FindBotFather(ctx, api)
		if err != nil {
			return err
		}

		// 1. Отправляем /setuserpic
		if err := mahalo.SendMessageWithRetry(ctx, api, botFather, "/setuserpic", 3); err != nil {
			return err
		}

		// 2. Ждем выбор бота
		resp, err := mahalo.WaitForResponseWithChecks(ctx, api, botFather,
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
		if err := mahalo.SendMessageWithRetry(ctx, api, botFather, botUsernameWithAt, 3); err != nil {
			return fmt.Errorf("не удалось отправить username бота: %w", err)
		}

		// 4. Ждем запрос фото
		if _, err := mahalo.WaitForResponseWithChecks(ctx, api, botFather,
			[]string{"send me the new profile photo", "profile photo", "photo for the bot", "ok. send me"},
			30*time.Second); err != nil {
			return fmt.Errorf("ожидание запроса фото: %w", err)
		}

		// 5. Отправляем фото
		if err := mahalo.SendPhoto(ctx, api, botFather, imagePath); err != nil {
			return err
		}

		// 6. Ждем подтверждение
		if _, err := mahalo.WaitForResponseWithChecks(ctx, api, botFather,
			[]string{"success", "updated", "done", "photo updated"},
			30*time.Second); err != nil {
			return fmt.Errorf("ожидание подтверждения: %w", err)
		}

		return nil
	})
}

func (c *Client) EnsureSession(ctx context.Context) (*telegram.Client, error) {
	client := telegram.NewClient(c.config.APIID, c.config.APIHash, telegram.Options{
		SessionStorage: &session.FileStorage{
			Path: c.config.SessionPath,
		},
	})

	// Создаем канал для результата
	resultChan := make(chan error, 1)

	err := client.Run(ctx, func(ctx context.Context) error {
		api := client.API()

		// Пробуем использовать существующую сессию
		_, err := api.HelpGetConfig(ctx)
		if err == nil {
			log.Printf("✅ Используем сохраненную сессию")
			resultChan <- nil
			return nil // Сессия работает
		}

		// Нужна авторизация
		fmt.Println("📱 Требуется авторизация...")

		// Удаляем старую сессию если она есть
		if _, err := os.Stat(c.config.SessionPath); err == nil {
			os.Remove(c.config.SessionPath)
		}

		flow := auth.NewFlow(
			auth.Constant(c.config.Phone, "", auth.CodeAuthenticatorFunc(
				func(ctx context.Context, sentCode *tg.AuthSentCode) (string, error) {
					fmt.Print("📱 Введите код из Telegram: ")
					reader := bufio.NewReader(os.Stdin)
					code, err := reader.ReadString('\n')
					if err != nil {
						return "", err
					}
					return strings.TrimSpace(code), nil
				},
			)),
			auth.SendCodeOptions{},
		)

		if err := client.Auth().IfNecessary(ctx, flow); err != nil {
			resultChan <- fmt.Errorf("авторизация не удалась: %w", err)
			return err
		}

		fmt.Println("✅ Успешно авторизованы!")
		resultChan <- nil
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Ждем завершения инициализации
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-resultChan:
		if err != nil {
			return nil, err
		}
		return client, nil
	}
}

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
