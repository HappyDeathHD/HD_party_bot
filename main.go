package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
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

type ReactionTypeEmoji struct {
	Type  string `json:"type"`
	Emoji string `json:"emoji"`
}

var banMap = make(map[string]bool)
var banMu sync.RWMutex

var textReplacements = make(map[string]string)
var textMu sync.RWMutex

func displayName(u *tgbotapi.User) string {
	if u == nil {
		return ""
	}
	if u.UserName != "" {
		return "@" + u.UserName
	}
	first := strings.TrimSpace(u.FirstName)
	last := strings.TrimSpace(u.LastName)
	return strings.TrimSpace(last + " " + first)
}

func isBanned(user string) bool {
	banMu.RLock()
	res := banMap[user]
	banMu.RUnlock()
	return res
}

func addBan(user string) {
	banMu.Lock()
	banMap[user] = true
	banMu.Unlock()
}

func removeBan(user string) {
	banMu.Lock()
	delete(banMap, user)
	banMu.Unlock()
}

func parseCmd(cmd string) (name string, limit int, date string, err error) {
	words := strings.Fields(cmd)
	if len(words) < 4 {
		return "", 0, "", fmt.Errorf("Используйте /сбор <название> <лимит> <дата> [время]")
	}

	limIdx := -1
	for i := len(words) - 2; i >= 1; i-- {
		if l, e := strconv.Atoi(words[i]); e == nil {
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
				val, err := strconv.Atoi(limitStr)
				if err != nil {
					return Rally{}, fmt.Errorf("invalid limit")
				}
				limit = val
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
			switch state {
			case "signed":
				parts := strings.SplitN(line, " ", 2)
				if len(parts) == 2 {
					id := strings.TrimSpace(parts[1])
					if id != "" {
						r.SignedUp = append(r.SignedUp, id)
					}
				}
			case "pencil":
				id := strings.TrimSpace(line)
				if id != "" {
					r.PenciledIn = append(r.PenciledIn, id)
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
		if i < len(r.SignedUp) {
			sb.WriteString(fmt.Sprintf("%d) %s\n", i+1, r.SignedUp[i]))
		} else {
			sb.WriteString(fmt.Sprintf("%d)\n", i+1))
		}
	}

	sb.WriteString("✏️ Карандашом:\n")
	for _, user := range r.PenciledIn {
		sb.WriteString(user)
		sb.WriteByte('\n')
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

func applyTextReplacementsConsume(text *string) (changed bool) {
	textMu.Lock()
	defer textMu.Unlock()

	for oldName, newName := range textReplacements {
		if oldName == "" {
			continue
		}
		if strings.Contains(*text, oldName) {
			*text = strings.ReplaceAll(*text, oldName, newName)
			delete(textReplacements, oldName)
			changed = true
		}
	}
	return changed
}

func setReaction(bot *tgbotapi.BotAPI, chatID int64, msgID int, emoji string) {
	reactionsJSON := fmt.Sprintf(`[{"type":"emoji","emoji":"%s"}]`, emoji)

	params := tgbotapi.Params{
		"chat_id":    strconv.FormatInt(chatID, 10),
		"message_id": strconv.Itoa(msgID),
		"reaction":   reactionsJSON,
	}

	if _, err := bot.MakeRequest("setMessageReaction", params); err != nil {
		log.Printf("setMessageReaction error: %v", err)
	}
}

func handleSudoRn(text string, userName string) (oldName, newName string, ok bool) {
	if userName != "@BulatHD" {
		return "", "", false
	}

	cmdPart := strings.TrimSpace(strings.TrimPrefix(text, "/sudo"))
	fields := strings.Fields(cmdPart)
	if len(fields) < 2 || fields[0] != "rn" {
		return "", "", false
	}

	rest := strings.TrimSpace(strings.TrimPrefix(cmdPart, "rn"))
	parts := strings.Split(rest, ":")
	if len(parts) < 2 {
		return "", "", false
	}

	mid := len(parts) / 2
	oldName = strings.TrimSpace(strings.Join(parts[:mid], ":"))
	newName = strings.TrimSpace(strings.Join(parts[mid:], ":"))
	if oldName == "" {
		return "", "", false
	}

	return oldName, newName, true
}

func handleSudoBanUnban(text, userName string) bool {
	if userName != "@BulatHD" {
		return false
	}

	cmdPart := strings.TrimSpace(strings.TrimPrefix(text, "/sudo"))
	fields := strings.Fields(cmdPart)
	if len(fields) < 2 {
		return false
	}

	switch fields[0] {
	case "ban":
		target := strings.TrimSpace(fields[1])
		if target == "" {
			return false
		}
		addBan(target)
		return true
	case "unban":
		target := strings.TrimSpace(fields[1])
		if target == "" {
			return false
		}
		removeBan(target)
		return true
	default:
		return false
	}
}

func parseUserInstance(entry string) (base string, n int, ok bool) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return "", 0, false
	}

	i := strings.LastIndexByte(entry, '+')
	if i == -1 {
		return entry, 0, true
	}

	basePart := strings.TrimSpace(entry[:i])
	numPart := strings.TrimSpace(entry[i+1:])
	if basePart == "" {
		return "", 0, false
	}
	if numPart == "" {
		return basePart, 0, true
	}

	val, err := strconv.Atoi(numPart)
	if err != nil || val < 0 {
		return "", 0, false
	}
	return basePart, val, true
}

func findUserInstances(list []string, user string) (indexes []int, numbers []int) {
	for i, e := range list {
		base, n, ok := parseUserInstance(e)
		if !ok {
			continue
		}
		if base == user {
			indexes = append(indexes, i)
			numbers = append(numbers, n)
		}
	}
	return
}

func addUserInstance(list []string, user string) []string {
	_, numbers := findUserInstances(list, user)
	maxN := 0
	for _, n := range numbers {
		if n > maxN {
			maxN = n
		}
	}

	if maxN == 0 && len(numbers) == 0 {
		return append(list, user)
	}
	return append(list, fmt.Sprintf("%s +%d", user, maxN+1))
}

func removeUserInstance(list []string, user string) []string {
	maxIdx := -1
	maxN := -1

	for i, e := range list {
		base, n, ok := parseUserInstance(e)
		if !ok || base != user {
			continue
		}
		if n > maxN {
			maxN = n
			maxIdx = i
		}
		if n == 0 && maxN == -1 {
			maxIdx = i
			maxN = 0
		}
	}

	if maxIdx == -1 {
		return list
	}

	res := make([]string, 0, len(list)-1)
	res = append(res, list[:maxIdx]...)
	res = append(res, list[maxIdx+1:]...)
	return res
}

func filterBanned(list []string) []string {
	res := make([]string, 0, len(list))
	for _, e := range list {
		base, _, ok := parseUserInstance(e)
		if !ok {
			res = append(res, e)
			continue
		}
		if isBanned(base) {
			continue
		}
		res = append(res, e)
	}
	return res
}

func editIgnoreNotModified(bot *tgbotapi.BotAPI, edit tgbotapi.EditMessageTextConfig) {
	if _, err := bot.Send(edit); err != nil {
		if strings.Contains(err.Error(), "message is not modified") {
			return
		}
		log.Printf("edit error: %v", err)
	}
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
		if update.Message != nil {
			text := strings.TrimSpace(update.Message.Text)
			chatID := update.Message.Chat.ID
			threadID := update.Message.MessageThreadID

			if strings.HasPrefix(text, "/sudo") {
				userName := displayName(update.Message.From)

				if oldName, newName, ok := handleSudoRn(text, userName); ok {
					textMu.Lock()
					textReplacements[oldName] = newName
					textMu.Unlock()
					setReaction(bot, chatID, update.Message.MessageID, "👍")
					continue
				}

				if handleSudoBanUnban(text, userName) {
					setReaction(bot, chatID, update.Message.MessageID, "👍")
				} else {
					setReaction(bot, chatID, update.Message.MessageID, "👎")
				}
				continue
			}

			if strings.HasPrefix(text, "/сбор") || strings.HasPrefix(text, "/party") {
				userName := displayName(update.Message.From)
				if isBanned(userName) {
					continue
				}

				name, limit, date, err := parseCmd(text)
				if err != nil {
					_, sendErr := bot.Send(tgbotapi.NewMessage(chatID, err.Error()))
					if sendErr != nil {
						log.Printf("send error: %v", sendErr)
					}
					continue
				}

				initiator := displayName(update.Message.From)

				rally := Rally{
					Name:       name,
					Date:       date,
					Limit:      limit,
					Initiator:  initiator,
					SignedUp:   []string{},
					PenciledIn: []string{},
					MessageID:  0,
					ChatID:     chatID,
				}

				msg := tgbotapi.NewMessage(chatID, formatRally(rally))
				msg.MessageThreadID = threadID
				msg.ReplyMarkup = buildKeyboard(rally, rally.Initiator)

				if _, err := bot.Send(msg); err != nil {
					log.Printf("send error: %v", err)
				}

				continue
			}
		}

		if update.CallbackQuery != nil {
			cb := update.CallbackQuery
			if cb.Message == nil {
				continue
			}

			user := displayName(cb.From)
			if user == "" || isBanned(user) {
				_ = sendSilentCallback(bot, cb.ID)
				continue
			}

			msgText := cb.Message.Text

			_ = applyTextReplacementsConsume(&msgText)

			rally, err := parseRally(msgText)
			if err != nil {
				log.Printf("parse rally error: %v", err)
				_ = sendSilentCallback(bot, cb.ID)
				continue
			}

			switch cb.Data {
			case "sign_up":
				if rally.Limit > 0 && len(rally.SignedUp) < rally.Limit {
					rally.SignedUp = addUserInstance(rally.SignedUp, user)

					filtered := make([]string, 0, len(rally.PenciledIn))
					for _, u := range rally.PenciledIn {
						base, _, ok := parseUserInstance(u)
						if !ok || base != user {
							filtered = append(filtered, u)
						}
					}
					rally.PenciledIn = filtered
				}

			case "unsign":
				rally.SignedUp = removeUserInstance(rally.SignedUp, user)
				rally.PenciledIn = removeUserInstance(rally.PenciledIn, user)

			case "sign_up_pencil":
				_, nums := findUserInstances(rally.SignedUp, user)
				_, numsP := findUserInstances(rally.PenciledIn, user)
				if len(nums) == 0 && len(numsP) == 0 {
					rally.PenciledIn = addUserInstance(rally.PenciledIn, user)
				}

			case "cancel":
				if user == rally.Initiator {
					rally.SignedUp = filterBanned(rally.SignedUp)
					rally.PenciledIn = filterBanned(rally.PenciledIn)

					edit := tgbotapi.NewEditMessageText(
						cb.Message.Chat.ID,
						cb.Message.MessageID,
						formatCancelledRally(rally),
					)
					kb := buildResumeKeyboard(rally, user)
					edit.ReplyMarkup = &kb
					editIgnoreNotModified(bot, edit)

					_ = sendCallback(bot, cb.ID, "Сбор отменён")
					continue
				}

			case "resume":
				if user == rally.Initiator {
					lines := strings.Split(cb.Message.Text, "\n")
					newText := cb.Message.Text
					if len(lines) > 1 && strings.TrimSpace(lines[0]) == "❌ СБОР ОТМЕНЁН ❌" {
						newText = strings.Join(lines[1:], "\n")
					}

					_ = applyTextReplacementsConsume(&newText)

					resumedRally, err := parseRally(newText)
					if err != nil {
						log.Printf("resume parse error: %v", err)
						resumedRally = rally
					}

					resumedRally.SignedUp = filterBanned(resumedRally.SignedUp)
					resumedRally.PenciledIn = filterBanned(resumedRally.PenciledIn)

					edit := tgbotapi.NewEditMessageText(
						cb.Message.Chat.ID,
						cb.Message.MessageID,
						formatRally(resumedRally),
					)
					kb := buildKeyboard(resumedRally, resumedRally.Initiator)
					edit.ReplyMarkup = &kb
					editIgnoreNotModified(bot, edit)

					_ = sendCallback(bot, cb.ID, "Сбор возобновлён")
					continue
				}
			}
			rally.SignedUp = filterBanned(rally.SignedUp)
			rally.PenciledIn = filterBanned(rally.PenciledIn)

			newText := formatRally(rally)
			if newText != cb.Message.Text {
				edit := tgbotapi.NewEditMessageText(
					cb.Message.Chat.ID,
					cb.Message.MessageID,
					newText,
				)
				kb := buildKeyboard(rally, rally.Initiator)
				edit.ReplyMarkup = &kb
				editIgnoreNotModified(bot, edit)
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
