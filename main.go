package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/ilpy20/telegram-bot-api/v7"
)

type Rally struct {
	Name       string
	Date       string
	Limit      int
	Initiator  string
	SignedUp   []string
	PenciledIn []string
	MessageID  int
	ChatID     int64
}

func parseCmd(cmd string) (name string, limit int, date string, err error) {
	words := strings.Fields(cmd)
	if len(words) < 4 {
		return "", 0, "", fmt.Errorf("Используйте /сбор <название> <лимит> <дата> [время] или /party ...")
	}

	var limIdx int = -1
	for i := len(words) - 2; i >= 1; i-- {
		if l, err := strconv.Atoi(words[i]); err == nil {
			limIdx = i
			limit = l
			break
		}
	}
	if limIdx == -1 {
		return "", 0, "", fmt.Errorf("не найден лимит")
	}

	name = strings.Join(words[1:limIdx], " ")
	date = strings.Join(words[limIdx+1:], " ")
	return
}

func cleanPrefix(line string) string {
	line = strings.TrimSpace(line)
	for _, prefix := range []string{
		"🎉", "📅", "🔢", "👤", "✍️", "✏️", "❌",
	} {
		if strings.HasPrefix(line, prefix) {
			line = strings.TrimSpace(line[len(prefix):])
		}
	}
	return line
}

func parseRally(message string) (Rally, error) {
	lines := strings.Split(message, "\n")
	r := Rally{}
	state := ""

	for i := 0; i < len(lines); i++ {
		line := cleanPrefix(strings.TrimSpace(lines[i]))
		switch {
		case strings.HasPrefix(line, "Сбор:"):
			r.Name = strings.TrimSpace(line[len("Сбор:"):])
		case strings.HasPrefix(line, "Дата:"):
			r.Date = strings.TrimSpace(line[len("Дата:"):])
		case strings.HasPrefix(line, "Лимит:"):
			limitStr := strings.TrimSpace(line[len("Лимит:"):])
			limit := 0
			if limitStr != "" {
				var err error
				limit, err = strconv.Atoi(limitStr)
				if err != nil {
					return Rally{}, fmt.Errorf("invalid limit")
				}
			}
			r.Limit = limit
		case strings.HasPrefix(line, "Инициатор:"):
			r.Initiator = strings.TrimSpace(line[len("Инициатор:"):])
		case strings.HasPrefix(line, "Записались:"):
			state = "signed"
		case strings.HasPrefix(line, "Карандашом:"):
			state = "pencil"
		case line == "":
		default:
			if state == "signed" && len(line) > 0 {
				parts := strings.Split(line, " ")
				if len(parts) == 2 && strings.HasPrefix(parts[1], "@") {
					r.SignedUp = append(r.SignedUp, parts[1])
				}
			} else if state == "pencil" && len(line) > 0 {
				if strings.HasPrefix(line, "@") {
					r.PenciledIn = append(r.PenciledIn, line)
				}
			}
		}
	}

	if r.Name == "" || r.Date == "" || r.Initiator == "" {
		return Rally{}, fmt.Errorf("missing fields in structure")
	}

	return r, nil
}

func formatRally(r Rally) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf(
		"🎉 Сбор: %s\n📅 Дата: %s\n🔢 Лимит: %d\n👤 Инициатор: %s\n\n✍️ Записались:\n",
		r.Name, r.Date, r.Limit, r.Initiator,
	))

	for i := 0; i < r.Limit; i++ {
		var line string
		if i < len(r.SignedUp) {
			line = fmt.Sprintf("%d) %s\n", i+1, r.SignedUp[i])
		} else {
			line = fmt.Sprintf("%d)\n", i+1)
		}
		sb.WriteString(line)
	}

	sb.WriteString("✏️ Карандашом:\n")
	for _, user := range r.PenciledIn {
		sb.WriteString(fmt.Sprintf("%s\n", user))
	}

	return sb.String()
}

func buildKeyboard(r Rally, userName string) tgbotapi.InlineKeyboardMarkup {
	buttons := [][]tgbotapi.InlineKeyboardButton{}

	if r.Limit > 0 && len(r.SignedUp) < r.Limit {
		buttons = append(buttons,
			[]tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData("✍️ Записаться ✍️", "sign_up"),
			},
		)
	}

	buttons = append(buttons,
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("🧽 Отписаться 🧽", "unsign"),
		},
	)

	buttons = append(buttons,
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("✏️ Карандашом ✏️", "sign_up_pencil"),
		},
	)

	if userName == r.Initiator {
		buttons = append(buttons,
			[]tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData("❌ Отменить ❌", "cancel"),
			},
		)
	}

	return tgbotapi.NewInlineKeyboardMarkup(buttons...)
}

func formatCancelledRally(r Rally) string {
	return "❌ СБОР ОТМЕНЁН ❌\n" + formatRally(r)
}

func buildResumeKeyboard(r Rally, userName string) tgbotapi.InlineKeyboardMarkup {
	buttons := [][]tgbotapi.InlineKeyboardButton{}

	if userName == r.Initiator {
		buttons = append(buttons,
			[]tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData("🔄 Возобновить 🔄", "resume"),
			},
		)
	}

	return tgbotapi.NewInlineKeyboardMarkup(buttons...)
}

func loadTitleMap(path string) map[string]string {
	m := make(map[string]string)

	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}

	lines := strings.Split(string(data), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		parts := strings.SplitN(l, ":", 2)
		if len(parts) != 2 {
			continue
		}
		oldName := strings.TrimSpace(parts[0])
		newName := strings.TrimSpace(parts[1])
		if oldName != "" && newName != "" {
			m[oldName] = newName
		}
	}

	return m
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	token := os.Getenv("TELEGRAM_APITOKEN")
	if token == "" {
		log.Panic("TELEGRAM_APITOKEN is empty")
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = false

	log.Printf("Bot authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil &&
			(strings.HasPrefix(update.Message.Text, "/сбор") || strings.HasPrefix(update.Message.Text, "/party")) {

			name, limit, date, err := parseCmd(update.Message.Text)
			if err != nil {
				_, sendErr := bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, err.Error()))
				if sendErr != nil {
					log.Printf("send error: %v", sendErr)
				}
				continue
			}

			initiator := ""
			if update.Message.From != nil && update.Message.From.UserName != "" {
				initiator = "@" + update.Message.From.UserName
			}

			rally := Rally{
				Name:       name,
				Date:       date,
				Limit:      limit,
				Initiator:  initiator,
				SignedUp:   []string{},
				PenciledIn: []string{},
				MessageID:  0,
				ChatID:     update.Message.Chat.ID,
			}

			msg := tgbotapi.NewMessage(update.Message.Chat.ID, formatRally(rally))
			msg.MessageThreadID = update.Message.MessageThreadID
			msg.ReplyMarkup = buildKeyboard(rally, rally.Initiator)

			sent, err := bot.Send(msg)
			if err != nil {
				log.Printf("send error: %v", err)
				continue
			}
			rally.MessageID = sent.MessageID

			continue
		}

		if update.CallbackQuery != nil {
			cb := update.CallbackQuery
			if cb.Message == nil {
				continue
			}
			message := cb.Message.Text

			rally, err := parseRally(message)
			if err != nil {
				log.Printf("parse rally error: %v", err)
				_ = sendSilentCallback(bot, cb.ID)
				continue
			}

			user := ""
			if cb.From != nil && cb.From.UserName != "" {
				user = "@" + cb.From.UserName
			}
			if user == "" {
				_ = sendSilentCallback(bot, cb.ID)
				continue
			}

			changed := false

			switch cb.Data {
			case "sign_up":
				found := false
				for _, u := range rally.SignedUp {
					if u == user {
						found = true
						break
					}
				}
				if !found && len(rally.SignedUp) < rally.Limit {
					rally.SignedUp = append(rally.SignedUp, user)

					var filtered []string
					for _, u := range rally.PenciledIn {
						if u != user {
							filtered = append(filtered, u)
						}
					}
					rally.PenciledIn = filtered
					changed = true
				}

			case "unsign":
				wasInSigned := false
				wasInPencil := false

				var filtered []string
				for _, u := range rally.SignedUp {
					if u == user {
						wasInSigned = true
						continue
					}
					filtered = append(filtered, u)
				}
				rally.SignedUp = filtered

				var filteredP []string
				for _, u := range rally.PenciledIn {
					if u == user {
						wasInPencil = true
						continue
					}
					filteredP = append(filteredP, u)
				}
				rally.PenciledIn = filteredP

				if wasInSigned || wasInPencil {
					changed = true
				}

			case "sign_up_pencil":
				found := false
				for _, u := range rally.SignedUp {
					if u == user {
						found = true
						break
					}
				}
				foundP := false
				for _, u := range rally.PenciledIn {
					if u == user {
						foundP = true
						break
					}
				}
				if !found && !foundP {
					rally.PenciledIn = append(rally.PenciledIn, user)
					changed = true
				}

			case "cancel":
				if user == rally.Initiator {
					edit := tgbotapi.NewEditMessageText(
						cb.Message.Chat.ID,
						cb.Message.MessageID,
						formatCancelledRally(rally),
					)
					kb := buildResumeKeyboard(rally, user)
					edit.ReplyMarkup = &kb
					if _, err := bot.Send(edit); err != nil {
						log.Printf("edit cancel error: %v", err)
					}
					_ = sendCallback(bot, cb.ID, "Сбор отменён")
					continue
				}

			case "resume":
				if user == rally.Initiator {
					lines := strings.Split(cb.Message.Text, "\n")
					newText := ""
					if len(lines) > 1 && strings.TrimSpace(lines[0]) == "❌ СБОР ОТМЕНЁН ❌" {
						newText = strings.Join(lines[1:], "\n")
					} else {
						newText = cb.Message.Text
					}

					resumedRally, err := parseRally(newText)
					if err != nil {
						log.Printf("resume parse error: %v", err)
						resumedRally = rally
					}

					titleMap := loadTitleMap("titles.txt")
					if newName, ok := titleMap[resumedRally.Name]; ok {
						resumedRally.Name = newName
					}

					edit := tgbotapi.NewEditMessageText(
						cb.Message.Chat.ID,
						cb.Message.MessageID,
						formatRally(resumedRally),
					)
					kb := buildKeyboard(resumedRally, user)
					edit.ReplyMarkup = &kb
					if _, err := bot.Send(edit); err != nil {
						log.Printf("edit resume error: %v", err)
					}
					_ = sendCallback(bot, cb.ID, "Сбор возобновлён")
					continue
				}
			}

			if changed {
				edit := tgbotapi.NewEditMessageText(
					cb.Message.Chat.ID,
					cb.Message.MessageID,
					formatRally(rally),
				)
				kb := buildKeyboard(rally, rally.Initiator)
				edit.ReplyMarkup = &kb

				if _, err := bot.Send(edit); err != nil {
					log.Printf("edit error: %v", err)
				}
			}

			_ = sendSilentCallback(bot, cb.ID)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func sendSilentCallback(bot *tgbotapi.BotAPI, id string) error {
	_, err := bot.Request(tgbotapi.NewCallback(id, ""))
	return err
}

func sendCallback(bot *tgbotapi.BotAPI, id, text string) error {
	_, err := bot.Request(tgbotapi.NewCallback(id, text))
	return err
}
