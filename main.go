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
	Name        string
	Date        string
	Limit       int
	Initiator   string
	SignedUp    []string
	WaitingList []string
	PenciledIn  []string
	MessageID   int
	ChatID      int64
}

const (
	ADMIN_USERNAME  = "@BulatHD"
	CMD_USAGE       = "Используйте /сбор <название> <лимит> <дата> [время]"
	LIMIT_MIN       = 2
	LIMIT_MAX       = 30
	LIMIT_RANGE_MSG = "Лимит должен быть от 2 до 30"

	MAX_PLUS_FRIENDS = 4
)

var (
	banList []string
	banMu   sync.RWMutex

	textReplacements = make(map[string]string)
	textMu           sync.RWMutex

	deleteOnCancel bool
	deleteMu       sync.RWMutex
)

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

func isAdmin(user string) bool {
	return user == ADMIN_USERNAME
}

func isBanned(user string) bool {
	banMu.RLock()
	defer banMu.RUnlock()
	for _, u := range banList {
		if u == user {
			return true
		}
	}
	return false
}

func addBan(user string) {
	banMu.Lock()
	defer banMu.Unlock()
	for _, u := range banList {
		if u == user {
			return
		}
	}
	banList = append(banList, user)
}

func removeBan(user string) {
	banMu.Lock()
	defer banMu.Unlock()
	res := make([]string, 0, len(banList))
	for _, u := range banList {
		if u != user {
			res = append(res, u)
		}
	}
	banList = res
}

func clearBans() {
	banMu.Lock()
	defer banMu.Unlock()
	banList = nil
}

func setDeleteOnCancel(v bool) {
	deleteMu.Lock()
	defer deleteMu.Unlock()
	deleteOnCancel = v
}

func getDeleteOnCancel() bool {
	deleteMu.RLock()
	defer deleteMu.RUnlock()
	return deleteOnCancel
}

func parseCmd(cmd string) (name string, limit int, date string, err error) {
	words := strings.Fields(cmd)
	if len(words) < 4 {
		return "", 0, "", fmt.Errorf(CMD_USAGE)
	}

	limIdx := -1
	for i := len(words) - 2; i >= 1; i-- {
		if l, e := strconv.Atoi(words[i]); e == nil {
			limIdx = i
			limit = l
			break
		}
	}
	if limIdx == -1 || limIdx < 2 {
		return "", 0, "", fmt.Errorf(CMD_USAGE)
	}

	name = strings.TrimSpace(strings.Join(words[1:limIdx], " "))
	if name == "" {
		return "", 0, "", fmt.Errorf(CMD_USAGE)
	}

	date = strings.Join(words[limIdx+1:], " ")
	if strings.TrimSpace(date) == "" {
		return "", 0, "", fmt.Errorf(CMD_USAGE)
	}

	return name, limit, date, nil
}

func cleanPrefix(line string) string {
	line = strings.TrimSpace(line)
	for _, prefix := range []string{
		"🎉", "📅", "🔢", "👤", "✍️", "✏️", "❌", "⏳",
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
		case strings.HasPrefix(line, "Лист ожидания:"):
			state = "waiting"
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
			case "waiting":
				parts := strings.SplitN(line, " ", 2)
				if len(parts) == 2 {
					id := strings.TrimSpace(parts[1])
					if id != "" {
						r.WaitingList = append(r.WaitingList, id)
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

	mainCount := len(r.SignedUp)
	for i := 0; i < r.Limit; i++ {
		if i < mainCount {
			sb.WriteString(fmt.Sprintf("%d) %s\n", i+1, r.SignedUp[i]))
		} else {
			sb.WriteString(fmt.Sprintf("%d)\n", i+1))
		}
	}

	if len(r.WaitingList) > 0 {
		sb.WriteString("\n⏳ Лист ожидания:\n")
		for i, user := range r.WaitingList {
			sb.WriteString(fmt.Sprintf("%d) %s\n", r.Limit+i+1, user))
		}
	}

	sb.WriteString("\n✏️ Карандашом:\n")
	for _, user := range r.PenciledIn {
		sb.WriteString(user)
		sb.WriteByte('\n')
	}

	return sb.String()
}

func buildKeyboard(r Rally, userName string) tgbotapi.InlineKeyboardMarkup {
	buttons := [][]tgbotapi.InlineKeyboardButton{}

	buttons = append(buttons,
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("✍️ Записаться ✍️", "sign_up"),
		},
	)

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

	if userName == r.Initiator || isAdmin(userName) {
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

	if userName == r.Initiator || isAdmin(userName) {
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
	if !isAdmin(userName) {
		return "", "", false
	}

	cmdPart := strings.TrimSpace(strings.TrimPrefix(text, "/sudo"))
	fields := strings.Fields(cmdPart)
	if len(fields) < 2 || fields[0] != "rn" {
		return "", "", false
	}

	rest := strings.TrimSpace(strings.TrimPrefix(cmdPart, "rn"))
	parts := strings.SplitN(rest, "||", 2)
	if len(parts) < 2 {
		return "", "", false
	}

	oldName = strings.TrimSpace(parts[0])
	newName = strings.TrimSpace(parts[1])
	if oldName == "" {
		return "", "", false
	}

	return oldName, newName, true
}

func handleSudoBanUnbanClearDelete(text, userName string) bool {
	if !isAdmin(userName) {
		return false
	}

	cmdPart := strings.TrimSpace(strings.TrimPrefix(text, "/sudo"))
	fields := strings.Fields(cmdPart)
	if len(fields) < 1 {
		return false
	}

	switch fields[0] {
	case "ban":
		if len(fields) < 2 {
			return false
		}
		target := strings.TrimSpace(fields[1])
		if target == "" {
			return false
		}
		addBan(target)
		return true

	case "unban":
		if len(fields) < 2 {
			return false
		}
		target := strings.TrimSpace(fields[1])
		if target == "" {
			return false
		}
		removeBan(target)
		return true

	case "clear":
		clearBans()
		textMu.Lock()
		textReplacements = make(map[string]string)
		textMu.Unlock()
		return true

	case "delete":
		setDeleteOnCancel(true)
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

func findAllUserNumbers(signed, waiting, penciled []string, user string) []int {
	var res []int
	for _, e := range signed {
		base, n, ok := parseUserInstance(e)
		if ok && base == user {
			res = append(res, n)
		}
	}
	for _, e := range waiting {
		base, n, ok := parseUserInstance(e)
		if ok && base == user {
			res = append(res, n)
		}
	}
	for _, e := range penciled {
		base, n, ok := parseUserInstance(e)
		if ok && base == user {
			res = append(res, n)
		}
	}
	return res
}

func findMaxNumberAll(signed, waiting, penciled []string, user string) int {
	nums := findAllUserNumbers(signed, waiting, penciled, user)
	maxN := 0
	for _, n := range nums {
		if n > maxN {
			maxN = n
		}
	}
	return maxN
}

func findMaxInstanceGlobal(signed, waiting, penciled []string, user string) (where string, idx int, n int, ok bool) {
	maxN := -1
	where = ""
	idx = -1

	for i, e := range signed {
		base, num, okParse := parseUserInstance(e)
		if !okParse || base != user {
			continue
		}
		if num > maxN {
			maxN = num
			where = "signed"
			idx = i
		}
	}

	for i, e := range waiting {
		base, num, okParse := parseUserInstance(e)
		if !okParse || base != user {
			continue
		}
		if num > maxN {
			maxN = num
			where = "waiting"
			idx = i
		}
	}

	for i, e := range penciled {
		base, num, okParse := parseUserInstance(e)
		if !okParse || base != user {
			continue
		}
		if num > maxN {
			maxN = num
			where = "pencil"
			idx = i
		}
	}

	if idx == -1 {
		return "", 0, 0, false
	}
	return where, idx, maxN, true
}

func addUserInstanceGlobal(target, signed, waiting, penciled []string, user string) []string {
	nums := findAllUserNumbers(signed, waiting, penciled, user)
	maxN := 0
	for _, n := range nums {
		if n > maxN {
			maxN = n
		}
	}
	if maxN >= MAX_PLUS_FRIENDS {
		return target
	}
	if maxN == 0 && len(nums) == 0 {
		return append(target, user)
	}
	return append(target, fmt.Sprintf("%s +%d", user, maxN+1))
}

func removeAtIndex(list []string, idx int) []string {
	if idx < 0 || idx >= len(list) {
		return list
	}
	return append(list[:idx], list[idx+1:]...)
}

func unsignGlobal(r *Rally, user string) {
	where, idx, _, ok := findMaxInstanceGlobal(r.SignedUp, r.WaitingList, r.PenciledIn, user)
	if !ok {
		return
	}
	switch where {
	case "signed":
		r.SignedUp = removeAtIndex(r.SignedUp, idx)
		if len(r.WaitingList) > 0 {
			firstWaiting := r.WaitingList[0]
			r.WaitingList = r.WaitingList[1:]
			r.SignedUp = append(r.SignedUp, firstWaiting)
		}
	case "waiting":
		r.WaitingList = removeAtIndex(r.WaitingList, idx)
	case "pencil":
		r.PenciledIn = removeAtIndex(r.PenciledIn, idx)
	}
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

func sendSilentCallback(bot *tgbotapi.BotAPI, id string) error {
	_, err := bot.Request(tgbotapi.NewCallback(id, ""))
	return err
}

func sendCallback(bot *tgbotapi.BotAPI, id, text string) error {
	_, err := bot.Request(tgbotapi.NewCallback(id, text))
	return err
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

				if handleSudoBanUnbanClearDelete(text, userName) {
					setReaction(bot, chatID, update.Message.MessageID, "👍")
				} else {
					setReaction(bot, chatID, update.Message.MessageID, "👎")
				}
				continue
			}

			if strings.HasPrefix(text, "/сбор") || strings.HasPrefix(text, "/party") {
				userName := displayName(update.Message.From)
				if isBanned(userName) {
					setReaction(bot, chatID, update.Message.MessageID, "👎")
					continue
				}

				name, limit, date, err := parseCmd(text)
				if err != nil {
					msg := tgbotapi.NewMessage(chatID, err.Error())
					msg.MessageThreadID = threadID
					_, sendErr := bot.Send(msg)
					if sendErr != nil {
						log.Printf("send error: %v", sendErr)
					}
					setReaction(bot, chatID, update.Message.MessageID, "👎")
					continue
				}

				if limit < LIMIT_MIN || limit > LIMIT_MAX {
					msg := tgbotapi.NewMessage(chatID, LIMIT_RANGE_MSG)
					msg.MessageThreadID = threadID
					_, sendErr := bot.Send(msg)
					if sendErr != nil {
						log.Printf("send error: %v", sendErr)
					}
					setReaction(bot, chatID, update.Message.MessageID, "👎")
					continue
				}

				initiator := displayName(update.Message.From)

				rally := Rally{
					Name:        name,
					Date:        date,
					Limit:       limit,
					Initiator:   initiator,
					SignedUp:    []string{},
					WaitingList: []string{},
					PenciledIn:  []string{},
					MessageID:   0,
					ChatID:      chatID,
				}

				msg := tgbotapi.NewMessage(chatID, formatRally(rally))
				msg.MessageThreadID = threadID
				msg.ReplyMarkup = buildKeyboard(rally, rally.Initiator)

				if _, err := bot.Send(msg); err != nil {
					log.Printf("send error: %v", err)
					setReaction(bot, chatID, update.Message.MessageID, "👎")
				} else {
					setReaction(bot, chatID, update.Message.MessageID, "👍")
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
				minIdx := -1
				minN := -1
				for i, e := range rally.PenciledIn {
					base, n, ok := parseUserInstance(e)
					if !ok || base != user {
						continue
					}
					if minN == -1 || n < minN {
						minN = n
						minIdx = i
					}
				}

				if minIdx != -1 {
					entry := ""
					if minN == 0 {
						entry = user
					} else {
						entry = fmt.Sprintf("%s +%d", user, minN)
					}
					if len(rally.SignedUp) < rally.Limit {
						rally.SignedUp = append(rally.SignedUp, entry)
					} else {
						rally.WaitingList = append(rally.WaitingList, entry)
					}
					rally.PenciledIn = removeAtIndex(rally.PenciledIn, minIdx)
				} else {
					currentMax := findMaxNumberAll(rally.SignedUp, rally.WaitingList, rally.PenciledIn, user)
					if currentMax >= MAX_PLUS_FRIENDS {
						_ = sendCallback(bot, cb.ID, fmt.Sprintf("Максимум %d друзей уже записано", MAX_PLUS_FRIENDS))
						break
					}

					if len(rally.SignedUp) < rally.Limit {
						rally.SignedUp = addUserInstanceGlobal(rally.SignedUp, rally.SignedUp, rally.WaitingList, rally.PenciledIn, user)
					} else {
						rally.WaitingList = addUserInstanceGlobal(rally.WaitingList, rally.SignedUp, rally.WaitingList, rally.PenciledIn, user)
					}
				}

			case "unsign":
				unsignGlobal(&rally, user)

			case "sign_up_pencil":
				currentMax := findMaxNumberAll(rally.SignedUp, rally.WaitingList, rally.PenciledIn, user)
				if currentMax >= MAX_PLUS_FRIENDS {
					_ = sendCallback(bot, cb.ID, fmt.Sprintf("Максимум %d друзей уже записано", MAX_PLUS_FRIENDS))
					break
				}
				rally.PenciledIn = addUserInstanceGlobal(rally.PenciledIn, rally.SignedUp, rally.WaitingList, rally.PenciledIn, user)

			case "cancel":
				if user == rally.Initiator || isAdmin(user) {
					if getDeleteOnCancel() && isAdmin(user) {
						setDeleteOnCancel(false)
						deleteMsg := tgbotapi.NewDeleteMessage(cb.Message.Chat.ID, cb.Message.MessageID)
						if _, err := bot.Request(deleteMsg); err != nil {
							log.Printf("delete message error: %v", err)
						}
						_ = sendCallback(bot, cb.ID, "Сообщение удалено")
						continue
					}

					rally.SignedUp = filterBanned(rally.SignedUp)
					rally.WaitingList = filterBanned(rally.WaitingList)
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
				if user == rally.Initiator || isAdmin(user) {
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
					resumedRally.WaitingList = filterBanned(resumedRally.WaitingList)
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
			rally.WaitingList = filterBanned(rally.WaitingList)
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
