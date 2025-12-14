package mahalo

import (
	"fmt"
	"strings"
)

// Ошибки BotFather
const (
	ErrUsernameTaken   = "username is already taken"
	ErrTooManyAttempts = "too many attempts"
	ErrInvalidUsername = "invalid"
	ErrBotNotFound     = "bot not found"
	ErrRateLimited     = "rate limited"
)

// ParseToken извлекает токен из сообщения BotFather
func ParseToken(message string) string {
	lines := strings.Split(message, "\n")

	for _, line := range lines {
		// Ищем строку с токеном (содержит двоеточие и достаточно длинная)
		if strings.Contains(line, ":") && len(line) > 30 {
			// Убираем возможные префиксы типа "Token: "
			cleanLine := strings.TrimSpace(line)

			// Разбиваем на слова и ищем токен
			words := strings.Fields(cleanLine)
			for _, word := range words {
				if strings.Contains(word, ":") && len(word) > 30 {
					// Убираем возможные пунктуации в конце
					token := strings.TrimRight(word, ".,!?")
					return token
				}
			}
		}
	}

	// Ищем вручную по символам
	for i := 0; i < len(message); i++ {
		if message[i] == ':' {
			// Ищем начало (цифры)
			start := i - 1
			for start >= 0 && message[start] >= '0' && message[start] <= '9' {
				start--
			}
			start++

			// Ищем конец (буквы/цифры)
			end := i + 1
			for end < len(message) &&
				((message[end] >= 'a' && message[end] <= 'z') ||
					(message[end] >= 'A' && message[end] <= 'Z') ||
					(message[end] >= '0' && message[end] <= '9')) {
				end++
			}

			if (i-start) >= 5 && (end-i) >= 20 { // Минимум 5 цифр и 20 символов
				token := message[start:end]
				return token
			}
		}
	}

	return ""
}

// IsPrompt проверяет, содержит ли сообщение определенные ключевые слова
func IsPrompt(message string, keywords []string) bool {
	msgLower := strings.ToLower(message)
	for _, keyword := range keywords {
		if strings.Contains(msgLower, keyword) {
			return true
		}
	}
	return false
}

// FormatCommands форматирует команды для BotFather
func FormatCommands(commands map[string]string) string {
	var builder strings.Builder
	for command, description := range commands {
		builder.WriteString(command + " - " + description + "\n")
	}
	return strings.TrimSpace(builder.String())
}

// CheckBotFatherError проверяет ответ BotFather на ошибки
func CheckBotFatherError(message string) error {
	msgLower := strings.ToLower(message)

	if strings.Contains(msgLower, "sorry, this username is already taken") ||
		strings.Contains(msgLower, "username is already taken") {
		return fmt.Errorf(ErrUsernameTaken)
	}

	if strings.Contains(msgLower, "too many attempts") ||
		strings.Contains(msgLower, "please try again in") {
		// Извлекаем время ожидания
		seconds := ExtractWaitTime(message)
		return fmt.Errorf("%s: %d seconds", ErrTooManyAttempts, seconds)
	}

	if strings.Contains(msgLower, "invalid username") ||
		strings.Contains(msgLower, "username invalid") {
		return fmt.Errorf(ErrInvalidUsername)
	}

	if strings.Contains(msgLower, "sorry, too many attempts") {
		return fmt.Errorf(ErrRateLimited)
	}

	return nil
}

// extractWaitTime извлекает время ожидания из сообщения об ошибке
func ExtractWaitTime(message string) int {
	// Ищем числа в сообщении
	words := strings.Fields(message)
	for _, word := range words {
		if strings.Contains(word, "seconds") {
			// Пробуем извлечь число перед "seconds"
			for i := 0; i < len(word); i++ {
				if word[i] >= '0' && word[i] <= '9' {
					start := i
					for i < len(word) && word[i] >= '0' && word[i] <= '9' {
						i++
					}
					secondsStr := word[start:i]
					var seconds int
					fmt.Sscanf(secondsStr, "%d", &seconds)
					return seconds
				}
			}
		}
	}
	return 60 // дефолтное значение
}
