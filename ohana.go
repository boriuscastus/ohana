package ohana

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/boriuscastus/ohana/mahalo"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

// Конфигурация
var (
	config *Config
)

// ========== ИНИЦИАЛИЗАЦИЯ ==========

// SetupConfig сохраняет конфиг
func SetupConfig(apiID int, apiHash, phone, sessionPath string) error {
	if sessionPath == "" {
		sessionPath = "telegram_session.json"
	}

	config = &Config{
		APIID:       apiID,
		APIHash:     apiHash,
		Phone:       phone,
		SessionPath: sessionPath,
	}

	return nil
}

// ========== ОСНОВНЫЕ ФУНКЦИИ ==========

// CreateBot создает нового бота с интерактивными повторными попытками
func CreateBot(name string) (username, token string, err error) {
	if config == nil {
		return "", "", fmt.Errorf("конфиг не инициализирован")
	}

	err = runClientWithAuthRetry(func(ctx context.Context, api *tg.Client, client *telegram.Client) error {
		log.Printf("✅ Клиент запущен")

		botFather, err := mahalo.FindBotFather(ctx, api)
		if err != nil {
			log.Printf("❌ Ошибка при поиске BotFather: %v", err)
			return fmt.Errorf("не удалось найти BotFather: %w", err)
		}
		log.Printf("✅ BotFather найден!")

		// 1. Отправляем /newbot
		if err := mahalo.SendMessageWithRetry(ctx, api, botFather, "/newbot", 3); err != nil {
			return err
		}

		// 2. Ждем запрос имени
		_, err = mahalo.WaitForResponseWithChecks(ctx, api, botFather,
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
		_, err = mahalo.WaitForResponseWithChecks(ctx, api, botFather,
			[]string{"choose a username", "username for your bot", "good. now let's choose"},
			30*time.Second)
		if err != nil {
			return fmt.Errorf("ожидание запроса username: %w", err)
		}

		// 5. Интерактивная попытка username с повторами
		maxUsernameAttempts := 5
		for attempt := 1; attempt <= maxUsernameAttempts; attempt++ {
			fmt.Print("📝 Введите username для бота (должен заканчиваться на 'bot'): ")
			reader := bufio.NewReader(os.Stdin)
			userUsername, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("ошибка при чтении username: %w", err)
			}
			userUsername = strings.TrimSpace(userUsername)

			// Валидация формата
			if !strings.HasSuffix(strings.ToLower(userUsername), "bot") {
				fmt.Printf("❌ Username должен заканчиваться на 'bot'\n")
				continue
			}

			username = userUsername

			// Отправляем username
			if err := mahalo.SendMessageWithRetry(ctx, api, botFather, username, 3); err != nil {
				return err
			}

			// 6. Ждем ответ
			resp, err := mahalo.WaitForResponseWithChecks(ctx, api, botFather,
				[]string{"done", "congratulations", "use this token", "sorry", "invalid", "already taken"},
				30*time.Second)
			if err != nil {
				return err
			}

			// Проверяем на ошибки
			if err := mahalo.CheckBotFatherError(resp); err != nil {
				if strings.Contains(err.Error(), mahalo.ErrUsernameTaken) {
					fmt.Printf("❌ Username '@%s' уже занят (попытка %d/%d)\n", username, attempt, maxUsernameAttempts)
					if attempt < maxUsernameAttempts {
						fmt.Println("🔁 Попробуйте другой username...")
						// Отправляем /newbot снова
						if err := mahalo.SendMessageWithRetry(ctx, api, botFather, "/newbot", 3); err != nil {
							return err
						}
						// Пропускаем запрос имени (он уже был)
						// Отправляем имя снова
						if err := mahalo.SendMessageWithRetry(ctx, api, botFather, name, 3); err != nil {
							return err
						}
						// Ждем запрос username снова
						if _, err := mahalo.WaitForResponseWithChecks(ctx, api, botFather,
							[]string{"choose a username", "username for your bot", "good. now let's choose"},
							30*time.Second); err != nil {
							return err
						}
						continue
					} else {
						return fmt.Errorf("не удалось найти свободный username после %d попыток", maxUsernameAttempts)
					}
				}
				return err
			}

			// Извлекаем токен
			token = mahalo.ParseToken(resp)
			if token == "" {
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

			fmt.Printf("✅ Бот @%s успешно создан!\n", username)
			
			// Пауза перед настройкой команд (BotFather может требовать времени)
			log.Printf("⏳ Ожидание 5 сек перед дальнейшими операциями...")
			time.Sleep(5 * time.Second)
			
			return nil
		}

		return fmt.Errorf("не удалось создать бота после %d попыток", maxUsernameAttempts)
	})

	return username, token, err
}

// CreateBotWithUsername создает бота программно, принимает username (без @)
func CreateBotWithUsername(name, userUsername string) (token string, err error) {
	if config == nil {
		return "", fmt.Errorf("конфиг не инициализирован")
	}
	err = runClientWithAuthRetry(func(ctx context.Context, api *tg.Client, client *telegram.Client) error {
		botFather, err := mahalo.FindBotFather(ctx, api)
		if err != nil {
			return fmt.Errorf("не удалось найти BotFather: %w", err)
		}

		if err := mahalo.SendMessageWithRetry(ctx, api, botFather, "/newbot", 3); err != nil {
			return err
		}

		if _, err := mahalo.WaitForResponseWithChecks(ctx, api, botFather,
			[]string{"choose a name", "how are we going to call", "alright, a new bot", "good. now let's choose"},
			30*time.Second); err != nil {
			return fmt.Errorf("ожидание запроса имени: %w", err)
		}

		if err := mahalo.SendMessageWithRetry(ctx, api, botFather, name, 3); err != nil {
			return err
		}

		if _, err := mahalo.WaitForResponseWithChecks(ctx, api, botFather,
			[]string{"choose a username", "username for your bot", "good. now let's choose"},
			30*time.Second); err != nil {
			return fmt.Errorf("ожидание запроса username: %w", err)
		}

		if err := mahalo.SendMessageWithRetry(ctx, api, botFather, userUsername, 3); err != nil {
			return err
		}

		resp, err := mahalo.WaitForResponseWithChecks(ctx, api, botFather,
			[]string{"done", "congratulations", "use this token", "sorry", "invalid", "already taken"},
			30*time.Second)
		if err != nil {
			return err
		}

		if err := mahalo.CheckBotFatherError(resp); err != nil {
			return err
		}

		token = mahalo.ParseToken(resp)
		if token == "" {
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

		// Пауза перед настройкой команд (BotFather может требовать времени)
		log.Printf("⏳ Ожидание 5 сек перед дальнейшими операциями...")
		time.Sleep(5 * time.Second)

		return nil
	})

	return token, err
}

// CreateBotWithAutoUsername пытается создать бота, автоматически перебирая варианты username
// baseUsername - базовый кусок имени (может содержать 'bot' или не содержать)
// maxAttempts - максимальное число попыток (включая первую)
func CreateBotWithAutoUsername(name, baseUsername string, maxAttempts int) (chosenUsername, token string, err error) {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}

	// Нормализуем базу
	base := strings.TrimSpace(baseUsername)
	baseLower := strings.ToLower(base)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var candidate string
		if attempt == 1 {
			candidate = base
		} else {
			// Добавляем суффикс числа перед 'bot' если нужно, иначе просто добавляем число
			if strings.HasSuffix(baseLower, "bot") {
				// вставим число перед последним "bot"
				idx := len(base) - 3
				candidate = base[:idx] + strconv.Itoa(attempt) + base[idx:]
			} else {
				candidate = base + strconv.Itoa(attempt) + "bot"
			}
		}

		// Убедимся, что candidate оканчивается на 'bot'
		if !strings.HasSuffix(strings.ToLower(candidate), "bot") {
			candidate = candidate + "bot"
		}

		token, err = CreateBotWithUsername(name, candidate)
		if err == nil {
			return candidate, token, nil
		}

		// Если username занят — пробуем дальше, иначе возвращаем ошибку
		if strings.Contains(err.Error(), mahalo.ErrUsernameTaken) {
			// continue
			continue
		}
		return "", "", err
	}

	return "", "", fmt.Errorf("не удалось найти свободный username после %d попыток", maxAttempts)
}

// Программные (неинтерактивные) функции настройки бота
func SetBotName(botUsername, newName string) error {
	return execBotFatherCommand(botUsername, "/setname", newName,
		[]string{"send me the new name", "choose a name", "what name"},
		[]string{"success", "updated", "done", "name updated"})
}

func SetBotDescription(botUsername, description string) error {
	return execBotFatherCommand(botUsername, "/setdescription", description,
		[]string{"send me the new description", "what description", "description for the bot"},
		[]string{"success", "updated", "done", "description updated"})
}

func SetBotAbout(botUsername, aboutText string) error {
	return execBotFatherCommand(botUsername, "/setabouttext", aboutText,
		[]string{"about", "send me", "new text", "about text"},
		[]string{"success", "updated", "done", "about section updated"})
}

func SetBotCommands(botUsername string, commands map[string]string) error {
	commandsText := mahalo.FormatCommands(commands)
	return execBotFatherCommand(botUsername, "/setcommands", commandsText,
		[]string{"send me a list of commands", "list of commands", "command1 - description"},
		[]string{"success", "updated", "done", "command list updated"})
}

func SetBotUserpic(botUsername, imagePath string) error {
	return execBotFatherPhotoInteractive(botUsername, imagePath)
}

func DeleteBot(botUsername string) error {
	return execBotFatherCommand(botUsername, "/deletebot", "Yes, I am totally sure.",
		[]string{"are you sure", "confirm", "delete this bot", "yes, i am totally sure"},
		[]string{"deleted", "successfully deleted", "bot has been deleted", "done", "bot is gone"})
}

// ========== ФУНКЦИИ НАСТРОЙКИ БОТА ==========

// SetBotNameInteractive изменяет имя бота интерактивно
func SetBotNameInteractive() error {
	fmt.Print("📝 Введите username бота (например: mybot): ")
	reader := bufio.NewReader(os.Stdin)
	botUsername, _ := reader.ReadString('\n')
	botUsername = strings.TrimSpace(botUsername)

	fmt.Print("📝 Введите новое имя бота: ")
	newName, _ := reader.ReadString('\n')
	newName = strings.TrimSpace(newName)

	return execBotFatherCommandInteractive(botUsername, "/setname", newName,
		[]string{"send me the new name", "choose a name", "what name"},
		[]string{"success", "updated", "done", "name updated"})
}

// SetBotDescriptionInteractive изменяет описание бота
func SetBotDescriptionInteractive() error {
	fmt.Print("📝 Введите username бота (например: mybot): ")
	reader := bufio.NewReader(os.Stdin)
	botUsername, _ := reader.ReadString('\n')
	botUsername = strings.TrimSpace(botUsername)

	fmt.Print("📝 Введите описание бота: ")
	description, _ := reader.ReadString('\n')
	description = strings.TrimSpace(description)

	return execBotFatherCommandInteractive(botUsername, "/setdescription", description,
		[]string{"send me the new description", "what description", "description for the bot"},
		[]string{"success", "updated", "done", "description updated"})
}

// SetBotAboutInteractive изменяет информацию "О боте"
func SetBotAboutInteractive() error {
	fmt.Print("📝 Введите username бота (например: mybot): ")
	reader := bufio.NewReader(os.Stdin)
	botUsername, _ := reader.ReadString('\n')
	botUsername = strings.TrimSpace(botUsername)

	fmt.Print("📝 Введите информацию 'О боте': ")
	aboutText, _ := reader.ReadString('\n')
	aboutText = strings.TrimSpace(aboutText)

	return execBotFatherCommandInteractive(botUsername, "/setabouttext", aboutText,
		[]string{"about", "send me", "new text", "about text"},
		[]string{"success", "updated", "done", "about section updated"})
}

// SetBotCommandsInteractive устанавливает команды бота
func SetBotCommandsInteractive() error {
	fmt.Print("📝 Введите username бота (например: mybot): ")
	reader := bufio.NewReader(os.Stdin)
	botUsername, _ := reader.ReadString('\n')
	botUsername = strings.TrimSpace(botUsername)

	fmt.Println("📝 Введите команды (формат: команда - описание, без ведущего '/').")
	fmt.Println("💡 Примеры:")
	fmt.Println("  start - запустить бота")
	fmt.Println("  help - получить помощь")
	fmt.Println("  settings - настройки")
	fmt.Println("(введите 'done' когда закончите)")

	var commands []string
	reader = bufio.NewReader(os.Stdin)
	for {
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "done" {
			break
		}
		if line != "" {
			commands = append(commands, line)
		}
	}

	if len(commands) == 0 {
		return fmt.Errorf("не введено ни одной команды")
	}

	commandsText := strings.Join(commands, "\n")
	return execBotFatherCommandInteractive(botUsername, "/setcommands", commandsText,
		[]string{"send me a list of commands", "list of commands", "command1 - description"},
		[]string{"success", "updated", "done", "command list updated"})
}

// SetBotUserpicInteractive устанавливает фото профиля бота
func SetBotUserpicInteractive() error {
	fmt.Print("📝 Введите username бота (например: mybot): ")
	reader := bufio.NewReader(os.Stdin)
	botUsername, _ := reader.ReadString('\n')
	botUsername = strings.TrimSpace(botUsername)

	fmt.Print("📸 Введите путь к изображению профиля: ")
	imagePath, _ := reader.ReadString('\n')
	imagePath = strings.TrimSpace(imagePath)

	return execBotFatherPhotoInteractive(botUsername, imagePath)
}

// DeleteBotInteractive удаляет бота
func DeleteBotInteractive() error {
	fmt.Print("📝 Введите username бота для удаления (например: mybot): ")
	reader := bufio.NewReader(os.Stdin)
	botUsername, _ := reader.ReadString('\n')
	botUsername = strings.TrimSpace(botUsername)

	fmt.Println("⚠️ Вы уверены, что хотите удалить этого бота? Введите 'yes' для подтверждения:")
	confirmation, _ := reader.ReadString('\n')
	confirmation = strings.TrimSpace(confirmation)

	if strings.ToLower(confirmation) != "yes" {
		fmt.Println("❌ Отменено")
		return nil
	}

	return execBotFatherCommandInteractive(botUsername, "/deletebot", "Yes, I am totally sure.",
		[]string{"are you sure", "confirm", "delete this bot", "yes, i am totally sure"},
		[]string{"deleted", "successfully deleted", "bot has been deleted", "done", "bot is gone"})
}

// ========== ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ ==========
// authorize выполняет авторизацию если нужно
func authorize(ctx context.Context, client *telegram.Client, api *tg.Client) error {
	log.Printf("🔐 Проверка авторизации...")

	// Если файл сессии старше TTL, считаем его устаревшим и удаляем.
	// Если файл есть и свежий — проверим его работоспособность выполнив маленький API вызов.
	const sessionTTL = 30 * 24 * time.Hour // 30 дней
	if config != nil {
		if fi, err := os.Stat(config.SessionPath); err == nil {
			if time.Since(fi.ModTime()) > sessionTTL {
				log.Printf("⚠️ Файл сессии старше %v, удаляем: %s", sessionTTL, config.SessionPath)
				_ = os.Remove(config.SessionPath)
			} else {
				// Сессия недавняя — проверим валидность ключа
				log.Printf("ℹ️ Сессия имеет возраст %v — проверяем её валидность", time.Since(fi.ModTime()))
				if api != nil {
					// Небольшой вызов для проверки авторизации
					if _, err := api.HelpGetConfig(ctx); err == nil {
						log.Printf("✅ Сессия валидна (HelpGetConfig)")
						return nil
					} else {
						log.Printf("⚠️ Проверка сессии не удалась: %v", err)
						// Если это ошибка, связанная с невалидным ключом — удаляем сессию и продолжим авторизацию
						if strings.Contains(err.Error(), "AUTH_KEY_UNREGISTERED") || strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "Unauthorized") {
							log.Printf("🔁 Сессия невалидна — удаляем файл сессии и повторяем авторизацию")
							_ = os.Remove(config.SessionPath)
							// continue to auth flow below
						} else {
							// Для прочих ошибок попробуем всё равно пройти авторизацию (чтобы восстановить состояние)
							log.Printf("ℹ️ Попробуем пройти поток авторизации несмотря на ошибку проверки")
						}
					}
				}
			}
		}
	}

	// Если мы здесь — выполняем стандартный поток авторизации
	log.Printf("📱 Начинаем процесс авторизации для номера: %s", config.Phone)
	flow := auth.NewFlow(
		auth.Constant(config.Phone, "", auth.CodeAuthenticatorFunc(
			func(ctx context.Context, sentCode *tg.AuthSentCode) (string, error) {
				fmt.Println("\n📨 Код отправлен на Telegram!")
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
		log.Printf("❌ Авторизация не удалась: %v", err)
		return fmt.Errorf("авторизация не удалась: %w", err)
	}

	fmt.Println("✅ Успешно авторизованы!")
	log.Printf("✅ Авторизация успешна")
	return nil
}

// execBotFatherCommand выполняет команду с BotFather
func execBotFatherCommand(botUsername, command, text string, waitKeywords, successKeywords []string) error {
	// Use the client-run wrapper that retries once on AUTH_KEY_UNREGISTERED
	return runClientWithAuthRetry(func(ctx context.Context, api *tg.Client, client *telegram.Client) error {
		botFather, err := mahalo.FindBotFather(ctx, api)
		if err != nil {
			return fmt.Errorf("не удалось найти BotFather: %w", err)
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

		if strings.Contains(strings.ToLower(resp), "not found") ||
			strings.Contains(strings.ToLower(resp), "no bot") {
			return fmt.Errorf("бот @%s не найден", botUsername)
		}

		// 3. Отправляем username бота
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

// runClientWithAuthRetry запускает клиент и выполняет действие; при обнаружении AUTH_KEY_UNREGISTERED
// удаляет файл сессии и повторяет один раз.
func runClientWithAuthRetry(action func(ctx context.Context, api *tg.Client, client *telegram.Client) error) error {
	if config == nil {
		return fmt.Errorf("конфиг не инициализирован")
	}

	attempts := 0
	for {
		client := telegram.NewClient(config.APIID, config.APIHash, telegram.Options{
			SessionStorage: &session.FileStorage{Path: config.SessionPath},
		})

		ctx := context.Background()
		err := client.Run(ctx, func(ctx context.Context) error {
			api := client.API()
			// authorize will re-auth if needed
			if err := authorize(ctx, client, api); err != nil {
				return err
			}
			return action(ctx, api, client)
		})

		if err == nil {
			return nil
		}

		// Если получили AUTH_KEY_UNREGISTERED — попробуем удалить сессию и повторить один раз
		if attempts == 0 && strings.Contains(err.Error(), "AUTH_KEY_UNREGISTERED") {
			log.Printf("⚠️ Обнаружена AUTH_KEY_UNREGISTERED, удаляю сессию и повторяю: %v", err)
			_ = os.Remove(config.SessionPath)
			attempts++
			continue
		}

		return err
	}
}

// execBotFatherCommandInteractive выполняет команду с BotFather с интерактивным вводом
func execBotFatherCommandInteractive(botUsername, command, text string, waitKeywords, successKeywords []string) error {
	return runClientWithAuthRetry(func(ctx context.Context, api *tg.Client, client *telegram.Client) error {
		botFather, err := mahalo.FindBotFather(ctx, api)
		if err != nil {
			return fmt.Errorf("не удалось найти BotFather: %w", err)
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

		if strings.Contains(strings.ToLower(resp), "not found") ||
			strings.Contains(strings.ToLower(resp), "no bot") {
			return fmt.Errorf("бот @%s не найден", botUsername)
		}

		// 3. Отправляем username бота
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

		fmt.Printf("✅ Операция успешно выполнена для бота @%s\n", botUsername)
		return nil
	})
}

// execBotFatherPhotoInteractive отправляет фото бота через BotFather интерактивно
func execBotFatherPhotoInteractive(botUsername, imagePath string) error {
	return runClientWithAuthRetry(func(ctx context.Context, api *tg.Client, client *telegram.Client) error {
		botFather, err := mahalo.FindBotFather(ctx, api)
		if err != nil {
			return fmt.Errorf("не удалось найти BotFather: %w", err)
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

		if strings.Contains(strings.ToLower(resp), "not found") ||
			strings.Contains(strings.ToLower(resp), "no bot") {
			return fmt.Errorf("бот @%s не найден", botUsername)
		}

		// 3. Отправляем username бота
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

		fmt.Printf("✅ Фото профиля успешно установлено для бота @%s\n", botUsername)
		return nil
	})
}

// Config содержит конфигурацию
type Config struct {
	APIID       int
	APIHash     string
	Phone       string
	SessionPath string
}
