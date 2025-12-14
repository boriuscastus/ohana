package mahalo

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// ========== ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ ==========

// generateRandomID генерирует случайный ID для сообщения
func GenerateRandomID() int64 {
	var buf [8]byte
	rand.Read(buf[:])
	return -int64(binary.LittleEndian.Uint64(buf[:]) & 0x7fffffffffffffff)
}

// findBotFather находит пользователя BotFather
func FindBotFather(ctx context.Context, api *tg.Client) (*tg.InputPeerUser, error) {
	log.Printf("🔍 Ищем BotFather...")
	resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
		Username: "BotFather",
	})
	if err != nil {
		log.Printf("❌ Ошибка при поиске BotFather: %v", err)
		return nil, fmt.Errorf("не удалось найти BotFather: %w", err)
	}

	log.Printf("📋 Найдено пользователей: %d", len(resolved.Users))
	var botFatherUser *tg.User
	for i, user := range resolved.Users {
		log.Printf("  User %d: %T", i, user)
		if u, ok := user.(*tg.User); ok {
			log.Printf("    ID: %d, Username: %s", u.ID, u.Username)
			if u.Username == "BotFather" {
				botFatherUser = u
				break
			}
		}
	}

	if botFatherUser == nil {
		log.Printf("❌ BotFather не найден в списке пользователей")
		return nil, fmt.Errorf("BotFather не найден")
	}

	log.Printf("✅ BotFather найден! ID: %d", botFatherUser.ID)
	return &tg.InputPeerUser{
		UserID:     botFatherUser.ID,
		AccessHash: botFatherUser.AccessHash,
	}, nil
}

// sendMessage отправляет сообщение
func SendMessage(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, text string) error {
	_, err := api.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:      peer,
		Message:   text,
		RandomID:  GenerateRandomID(),
		NoWebpage: true,
	})

	if err != nil {
		return fmt.Errorf("не удалось отправить сообщение: %w", err)
	}

	log.Printf("📤 Отправлено: %s", text)
	time.Sleep(1 * time.Second)
	return nil
}

// getLastMessage получает последнее сообщение от собеседника
func GetLastMessage(ctx context.Context, api *tg.Client, peer tg.InputPeerClass) (string, error) {
	history, err := api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:  peer,
		Limit: 1,
	})

	if err != nil {
		return "", fmt.Errorf("не удалось получить историю: %w", err)
	}

	// Обрабатываем разные типы ответов
	switch h := history.(type) {
	case *tg.MessagesChannelMessages:
		if len(h.Messages) > 0 {
			if msg, ok := h.Messages[0].(*tg.Message); ok && !msg.Out {
				log.Printf("📥 Получено: %s", msg.Message)
				return msg.Message, nil
			}
		}
	case *tg.MessagesMessages:
		if len(h.Messages) > 0 {
			if msg, ok := h.Messages[0].(*tg.Message); ok && !msg.Out {
				log.Printf("📥 Получено: %s", msg.Message)
				return msg.Message, nil
			}
		}
	case *tg.MessagesMessagesSlice:
		if len(h.Messages) > 0 {
			if msg, ok := h.Messages[0].(*tg.Message); ok && !msg.Out {
				log.Printf("📥 Получено: %s", msg.Message)
				return msg.Message, nil
			}
		}
	}

	return "", nil
}

// waitForResponseWithChecks ждет ответ с проверкой ошибок
func WaitForResponseWithChecks(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, keywords []string, timeout time.Duration) (string, error) {
	deadline := time.After(timeout)

	for {
		select {
		case <-deadline:
			return "", fmt.Errorf("таймаут ожидания ответа (ключевые слова: %v)", keywords)
		case <-ctx.Done():
			return "", ctx.Err()
		default:
			msg, err := GetLastMessage(ctx, api, peer)
			if err != nil {
				return "", err
			}

			// Проверяем на ошибки BotFather
			if err := CheckBotFatherError(msg); err != nil {
				// Если это "too many attempts" — ждём указанное время и повторяем
				if strings.Contains(err.Error(), ErrTooManyAttempts) {
					// Извлекаем время ожидания
					seconds := ExtractWaitTime(msg)
					if seconds > 0 {
						log.Printf("⏳ BotFather требует подождать %d сек, ожидаем...", seconds)
						time.Sleep(time.Duration(seconds) * time.Second)
						// Сбрасываем дедлайн и повторяем попытку
						deadline = time.After(timeout)
						continue
					}
				}
				return "", err
			}

			if IsPrompt(msg, keywords) {
				return msg, nil
			}

			time.Sleep(2 * time.Second)
		}
	}
}

// sendMessageWithRetry отправляет сообщение с повторными попытками
func SendMessageWithRetry(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, text string, maxRetries int) error {
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		err := SendMessage(ctx, api, peer, text)
		if err == nil {
			return nil
		}

		lastErr = err

		// Проверяем, не слишком ли много попыток
		errStr := err.Error()
		if strings.Contains(errStr, ErrTooManyAttempts) ||
			strings.Contains(errStr, ErrRateLimited) {
			return err
		}

		// Ждем перед следующей попыткой
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i+1) * time.Second):
			continue
		}
	}

	return fmt.Errorf("не удалось отправить сообщение после %d попыток: %w", maxRetries, lastErr)
}

// sendPhoto отправляет фото
func SendPhoto(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, filePath string) error {
	// Открываем файл
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("не удалось открыть файл: %w", err)
	}
	defer file.Close()

	// Проверяем размер
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("не удалось получить информацию о файле: %w", err)
	}

	if fileInfo.Size() > 10*1024*1024 { // 10 MB
		return fmt.Errorf("файл слишком большой (максимум 10 MB)")
	}

	filename := filepath.Base(filePath)
	log.Printf("📤 Отправляем фото: %s (%.2f MB)", filename,
		float64(fileInfo.Size())/1024/1024)

	// Создаем uploader
	upd := uploader.NewUploader(api)

	// Загружаем файл
	upload, err := upd.Upload(ctx, uploader.NewUpload(filename, file, fileInfo.Size()))
	if err != nil {
		return fmt.Errorf("ошибка загрузки: %w", err)
	}

	// Отправляем как Photo
	_, err = api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
		Peer: peer,
		Media: &tg.InputMediaUploadedPhoto{
			File: upload,
		},
		Message:  " ",
		RandomID: GenerateRandomID(),
	})

	if err != nil {
		return fmt.Errorf("не удалось отправить фото: %w", err)
	}

	log.Printf("✅ Фото отправлено")
	return nil
}
