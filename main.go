package main

import (
	"time"
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
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
	ADMIN_USERNAME   = "@BulatHD"
	CMD_USAGE        = "Используйте /сбор <название> <лимит> <дата> [время]"
	LIMIT_MIN        = 2
	LIMIT_MAX        = 30
	LIMIT_RANGE_MSG  = "Лимит должен быть от 2 до 30"
	MAX_PLUS_FRIENDS = 4
)

var (
	banList          []string
	banMu            sync.RWMutex
	textReplacements = make(map[string]string)
	textMu           sync.RWMutex
	deleteOnCancel   bool
	deleteMu         sync.RWMutex
	lastEditTime 	 time.Time
)

func displayName(u *telego.User) string {
	if u == nil {
		return ""
	}
	if u.Username != "" {
		return "@" + u.Username
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
	for _, prefix := range []string{"🎉", "📅", "🔢", "👤", "✍️", "✏️", "❌", "⏳"} {
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
			continue
		default:
			switch state {
			case "signed", "waiting":
				parts := strings.SplitN(line, " ", 2)
				if len(parts) == 2 {
					id := strings.TrimSpace(parts[1])
					if id != "" {
						if state == "signed" {
							r.SignedUp = append(r.SignedUp, id)
						} else {
							r.WaitingList = append(r.WaitingList, id)
						}
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
		"<tg-emoji emoji-id=\"5310228579009699834\">🎉</tg-emoji> Сбор: %s\n<tg-emoji emoji-id=\"5433614043006903194\">📅</tg-emoji> Дата: %s\n<tg-emoji emoji-id=\"5373335654476294839\">🔢</tg-emoji> Лимит: %d\n<tg-emoji emoji-id=\"5373012449597335010\">👤</tg-emoji> Инициатор: %s\n\n<tg-emoji emoji-id=\"5470060791883374114\">✍️</tg-emoji> Записались:\n",
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
		sb.WriteString("\n<tg-emoji emoji-id=\"5451646226975955576\">⏳</tg-emoji> Лист ожидания:\n")
		for i, user := range r.WaitingList {
			sb.WriteString(fmt.Sprintf("%d) %s\n", r.Limit+i+1, user))
		}
	}
	sb.WriteString("\n<tg-emoji emoji-id=\"5334673106202010226\">✏️</tg-emoji> Карандашом:\n")
	for _, user := range r.PenciledIn {
		sb.WriteString(user + "\n")
	}
	return sb.String()
}

func buildKeyboard(r Rally, userName string) *telego.InlineKeyboardMarkup {
    return tu.InlineKeyboard(
        tu.InlineKeyboardRow(
            tu.InlineKeyboardButton("Записаться").
                WithCallbackData("sign_up").
                WithStyle("success").
                WithIconCustomEmojiID("5470060791883374114"),
            tu.InlineKeyboardButton("Карандашом").
                WithCallbackData("sign_up_pencil").
                WithStyle("primary").
                WithIconCustomEmojiID("5334673106202010226"),
        ),
        tu.InlineKeyboardRow(
            tu.InlineKeyboardButton("Отписаться").
                WithCallbackData("unsign").
                WithIconCustomEmojiID("5188365693803830912"),
            tu.InlineKeyboardButton("Отменить").
                WithCallbackData("cancel").
                WithStyle("danger").
                WithIconCustomEmojiID("5465665476971471368"),
        ),
    )
}

func buildResumeKeyboard(r Rally, userName string) *telego.InlineKeyboardMarkup {
	if userName != r.Initiator && !isAdmin(userName) {
		return nil
	}
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("Возобновить").
				WithCallbackData("resume").
				WithStyle("primary").
				WithIconCustomEmojiID("5264727218734524899"),
		),
	)
}

func formatCancelledRally(r Rally) string {
	return "❌ СБОР ОТМЕНЁН ❌\n" + formatRally(r)
}

func applyTextReplacementsConsume(text *string) bool {
	textMu.Lock()
	defer textMu.Unlock()
	changed := false
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

func setReaction(bot *telego.Bot, ctx context.Context, chatID int64, msgID int, emoji string) {
	token := bot.Token()
	url := fmt.Sprintf("https://api.telegram.org/bot%s/setMessageReaction", token)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": msgID,
		"reaction": []map[string]string{
			{
				"type":  "emoji",
				"emoji": emoji,
			},
		},
	}

	bodyBytes, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, _ := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
}

func editIgnoreNotModified(bot *telego.Bot, ctx context.Context, editParams *telego.EditMessageTextParams) {
    for {
        now := time.Now()
        if now.Sub(lastEditTime) >= 1100*time.Millisecond {
            break
        }
        time.Sleep(100 * time.Millisecond)
    }

    _, err := bot.EditMessageText(ctx, editParams)
    if err == nil {
        lastEditTime = time.Now()
    }
}

func sendSilentCallback(bot *telego.Bot, ctx context.Context, callbackID string) {
	_ = bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: callbackID,
	})
}

func sendCallback(bot *telego.Bot, ctx context.Context, callbackID, text string) {
	_ = bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: callbackID,
		Text:            text,
	})
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

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	token := os.Getenv("TELEGRAM_APITOKEN")
	if token == "" {
		log.Panic("TELEGRAM_APITOKEN is empty")
	}

	bot, err := telego.NewBot(token)
	if err != nil {
		log.Panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	me, err := bot.GetMe(ctx)
	if err != nil {
		log.Panic(err)
	}
	log.Printf("Bot authorized on account @%s", me.Username)

	updates, err := bot.UpdatesViaLongPolling(
    ctx,
    &telego.GetUpdatesParams{
        Timeout: 120,
        Limit:   100,
        Offset:  0,
    },
    telego.WithLongPollingRetryTimeout(10 * time.Second),
)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	for update := range updates {
		if update.Message != nil {
			msg := update.Message
			text := strings.TrimSpace(msg.Text)
			chatID := msg.Chat.ID
			threadID := msg.MessageThreadID
			userName := displayName(msg.From)

			if strings.HasPrefix(text, "/sudo") {
				if oldName, newName, ok := handleSudoRn(text, userName); ok {
					textMu.Lock()
					textReplacements[oldName] = newName
					textMu.Unlock()
					setReaction(bot, ctx, chatID, msg.MessageID, "👍")
					continue
				}
				if handleSudoBanUnbanClearDelete(text, userName) {
					setReaction(bot, ctx, chatID, msg.MessageID, "👍")
				} else {
					setReaction(bot, ctx, chatID, msg.MessageID, "👎")
				}
				continue
			}

			if strings.HasPrefix(text, "/сбор") || strings.HasPrefix(text, "/party") {
				if isBanned(userName) {
					setReaction(bot, ctx, chatID, msg.MessageID, "👎")
					continue
				}

				name, limit, date, err := parseCmd(text)
				if err != nil {
					_, _ = bot.SendMessage(ctx, &telego.SendMessageParams{
						ChatID:          tu.ID(chatID),
						Text:            err.Error(),
						MessageThreadID: threadID,
					})
					setReaction(bot, ctx, chatID, msg.MessageID, "👎")
					continue
				}

				if limit < LIMIT_MIN || limit > LIMIT_MAX {
					_, _ = bot.SendMessage(ctx, &telego.SendMessageParams{
						ChatID:          tu.ID(chatID),
						Text:            LIMIT_RANGE_MSG,
						MessageThreadID: threadID,
					})
					setReaction(bot, ctx, chatID, msg.MessageID, "👎")
					continue
				}

				initiator := userName
				rally := Rally{
					Name:      name,
					Date:      date,
					Limit:     limit,
					Initiator: initiator,
					ChatID:    chatID,
				}

				_, err = bot.SendMessage(ctx, &telego.SendMessageParams{
					ChatID:      tu.ID(chatID),
					Text:        formatRally(rally),
					ParseMode:	"HTML",
					MessageThreadID: threadID,
					ReplyMarkup: buildKeyboard(rally, rally.Initiator),
				})
				if err != nil {
					log.Printf("send error: %v", err)
					setReaction(bot, ctx, chatID, msg.MessageID, "👎")
				} else {
					setReaction(bot, ctx, chatID, msg.MessageID, "👍")
				}
				continue
			}
		}

		if update.CallbackQuery != nil {
			cb := update.CallbackQuery

			if cb.Message == nil {
				sendSilentCallback(bot, ctx, cb.ID)
				continue
			}

			msg := cb.Message.Message()
			if msg == nil {
				sendSilentCallback(bot, ctx, cb.ID)
				continue
			}

			user := displayName(&cb.From)
			if user == "" || isBanned(user) {
				sendSilentCallback(bot, ctx, cb.ID)
				continue
			}

			msgText := msg.Text
			_ = applyTextReplacementsConsume(&msgText)

			rally, err := parseRally(msgText)
			if err != nil {
				log.Printf("parse rally error: %v", err)
				sendSilentCallback(bot, ctx, cb.ID)
				continue
			}

			edited := false
			var newText string
			var newMarkup *telego.InlineKeyboardMarkup

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
					entry := user
					if minN != 0 {
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
						sendCallback(bot, ctx, cb.ID, fmt.Sprintf("Максимум %d друзей уже записано", MAX_PLUS_FRIENDS))
						break
					}
					if len(rally.SignedUp) < rally.Limit {
						rally.SignedUp = addUserInstanceGlobal(rally.SignedUp, rally.SignedUp, rally.WaitingList, rally.PenciledIn, user)
					} else {
						rally.WaitingList = addUserInstanceGlobal(rally.WaitingList, rally.SignedUp, rally.WaitingList, rally.PenciledIn, user)
					}
				}
				edited = true

			case "unsign":
				unsignGlobal(&rally, user)
				edited = true

			case "sign_up_pencil":
				currentMax := findMaxNumberAll(rally.SignedUp, rally.WaitingList, rally.PenciledIn, user)
				if currentMax >= MAX_PLUS_FRIENDS {
					sendCallback(bot, ctx, cb.ID, fmt.Sprintf("Максимум %d друзей уже записано", MAX_PLUS_FRIENDS))
					break
				}
				rally.PenciledIn = addUserInstanceGlobal(rally.PenciledIn, rally.SignedUp, rally.WaitingList, rally.PenciledIn, user)
				edited = true

			case "cancel":
				if user == rally.Initiator || isAdmin(user) {
					if getDeleteOnCancel() && isAdmin(user) {
						setDeleteOnCancel(false)
						_ = bot.DeleteMessage(ctx, &telego.DeleteMessageParams{
							ChatID:    tu.ID(msg.Chat.ID),
							MessageID: msg.MessageID,
						})
						sendCallback(bot, ctx, cb.ID, "Сообщение удалено")
						continue
					}
					rally.SignedUp = filterBanned(rally.SignedUp)
					rally.WaitingList = filterBanned(rally.WaitingList)
					rally.PenciledIn = filterBanned(rally.PenciledIn)
					newText = formatCancelledRally(rally)
					newMarkup = buildResumeKeyboard(rally, user)
					edited = true
					sendCallback(bot, ctx, cb.ID, "Сбор отменён")
				}

			case "resume":
				if user == rally.Initiator || isAdmin(user) {
					lines := strings.Split(msg.Text, "\n")
					newText = msg.Text
					if len(lines) > 1 && strings.TrimSpace(lines[0]) == "❌ СБОР ОТМЕНЁН ❌" {
						newText = strings.Join(lines[1:], "\n")
					}
					_ = applyTextReplacementsConsume(&newText)
					resumedRally, err := parseRally(newText)
					if err != nil {
						resumedRally = rally
					}
					resumedRally.SignedUp = filterBanned(resumedRally.SignedUp)
					resumedRally.WaitingList = filterBanned(resumedRally.WaitingList)
					resumedRally.PenciledIn = filterBanned(resumedRally.PenciledIn)
					newText = formatRally(resumedRally)
					newMarkup = buildKeyboard(resumedRally, resumedRally.Initiator)
					edited = true
					sendCallback(bot, ctx, cb.ID, "Сбор возобновлён")
				}
			}

			if edited {
				rally.SignedUp = filterBanned(rally.SignedUp)
				rally.WaitingList = filterBanned(rally.WaitingList)
				rally.PenciledIn = filterBanned(rally.PenciledIn)

				if newText == "" {
					newText = formatRally(rally)
				}
				if newMarkup == nil {
					newMarkup = buildKeyboard(rally, rally.Initiator)
				}

				if newText != msg.Text || newMarkup != nil {
					editParams := &telego.EditMessageTextParams{
						ChatID:      tu.ID(msg.Chat.ID),
						MessageID:   msg.MessageID,
						Text:        newText,
						ParseMode:   "HTML",
						ReplyMarkup: newMarkup,
					}
					editIgnoreNotModified(bot, ctx, editParams)
				}
			}

			sendSilentCallback(bot, ctx, cb.ID)
		}
	}
}