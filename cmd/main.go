package main

import (
	"fmt"
	"log"

	"github.com/boriuscastus/ohana"
)

func main() {
	apiID := 12345678
	apiHash := "3h2ujgjh43h5g4khjj5nn3l"
	phone := "+77777777777"

	// Настройте эти переменные в коде — не в терминале
	nonInteractive := true // <- переключатель режима

	// Значения для неинтерактивного запуска (заполните перед запуском)
	botName := "AnkaraRonaldo"
	// Можно задать базу для автоподбора или конкретный username
	// Заменил на валидный username, который вы указали для теста
	baseUsername := "odlanoraraknabot"
	description := "Bot for footbal"
	aboutText := "bot about football"
	commands := map[string]string{"/show": "Start", "/close": "Help"}
	imagePath := "C:\\Users\\BorBor\\Pictures\\ronaldo.jpg" // укажите реальный путь к файлу

	fmt.Println("=== OHANA — demo ===")

	// Инициализация
	if err := ohana.SetupConfig(apiID, apiHash, phone, "test_session.json"); err != nil {
		log.Fatalf("failed setup config: %v", err)
	}

	if nonInteractive {
		fmt.Println("Запуск неинтерактивной последовательности:")

		// 1) Создаем бота (с автоподбором username)
		username, token, err := ohana.CreateBotWithAutoUsername(botName, baseUsername, 10)
		if err != nil {
			log.Fatalf("CreateBot error: %v", err)
		}
		fmt.Printf("Создан бот: @%s, token=%s\n", username, token)

		// 2) Устанавливаем описание
		if err := ohana.SetBotDescription(username, description); err != nil {
			log.Printf("SetBotDescription error: %v", err)
		} else {
			fmt.Println("Описание установлено")
		}

		// 3) Устанавливаем информацию 'О боте'
		if err := ohana.SetBotAbout(username, aboutText); err != nil {
			log.Printf("SetBotAbout error: %v", err)
		} else {
			fmt.Println("Информация 'О боте' установлена")
		}

		// 4) Устанавливаем команды
		if err := ohana.SetBotCommands(username, commands); err != nil {
			log.Printf("SetBotCommands error: %v", err)
		} else {
			fmt.Println("Команды установлены")
		}

		// 5) Устанавливаем фото профиля (если файл есть)
		if imagePath != "" {
			if err := ohana.SetBotUserpic(username, imagePath); err != nil {
				log.Printf("SetBotUserpic error: %v", err)
			} else {
				fmt.Println("Фото профиля установлено")
			}
		}

		// 6) Удаляем бота
		if err := ohana.DeleteBot(username); err != nil {
			log.Printf("DeleteBot error: %v", err)
		} else {
			fmt.Println("Бот удалён")
		}

		return
	}

	// Если nonInteractive == false — остаётся старое интерактивное меню (не изменялось)
	fmt.Println("Интерактивный режим включён — запустите nonInteractive=true для demo в коде")
}
