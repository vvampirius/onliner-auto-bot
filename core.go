package main

import (
	"fmt"
	"github.com/mmcdole/gofeed"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/vvampirius/mygolibs/telegram"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

type Core struct {
	ConfigFile  *ConfigFile
	TelegramApi *telegram.Api
	State       *State
}

// GetNewItems возвращает список новых итемов относительно core.State.LastDate и последнее время побликации из них (если
// новых нет - будет IsZero).
func (core *Core) GetNewItems(items []*gofeed.Item) ([]*gofeed.Item, time.Time) {
	lastDate := core.State.LastDate
	var newLastDate time.Time
	newItems := make([]*gofeed.Item, 0)
	for _, item := range items {
		if item.PublishedParsed == nil {
			ErrorLog.Println(`Can't parse`, item.PublishedParsed)
			PrometheusErrors.With(prometheus.Labels{`action`: `get_item_date`}).Inc()
			continue
		}
		if item.PublishedParsed.After(lastDate) {
			newItems = append(newItems, item)
			if item.PublishedParsed.After(newLastDate) {
				newLastDate = *item.PublishedParsed
			}
		}
	}
	return newItems, newLastDate
}

func (core *Core) ReverseItems(items []*gofeed.Item) []*gofeed.Item {
	itemsCount := len(items)
	if itemsCount <= 1 {
		return items
	}
	newItems := make([]*gofeed.Item, 0)
	for i := itemsCount - 1; i >= 0; i-- {
		newItems = append(newItems, items[i])
	}
	return newItems
}

func (core *Core) RssHttpHandler(w http.ResponseWriter, r *http.Request) {
	DebugLog.Println(r.Method, r.RequestURI, r.UserAgent())
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	parser := gofeed.NewParser()
	feed, err := parser.Parse(r.Body)
	if err != nil {
		ErrorLog.Println(err.Error())
		PrometheusErrors.With(prometheus.Labels{`action`: `parse_rss`}).Inc()
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	go func() {
		DebugLog.Println(`Last date:`, core.State.LastDate.Format("02.01 15:04:05 MST"))
		items, newLastDate := core.GetNewItems(feed.Items)
		PrometheusNewItems.Add(float64(len(items)))
		for _, item := range core.ReverseItems(items) {
			DebugLog.Printf("%s / %v %s %s\n", item.PublishedParsed.Format("02.01 15:04:05 MST"), item.Categories, item.Title, item.Link)
			core.State.AddCategory(item.Categories...)
			core.SendItem(item.Title, item.Link, item.Categories)
		}
		if newLastDate.After(core.State.LastDate) {
			core.State.LastDate = newLastDate
			if err := core.State.Save(); err != nil {
				ErrorLog.Println(err.Error())
			}
		}
	}()
}

func (core *Core) GetUser(id int) (*User, error) {
	user, err := NewUser(path.Join(core.ConfigFile.Config.BaseDir, `users`, fmt.Sprintf("%d.yml", id)))
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (core *Core) GetOrCreateUser(info telegram.User) (*User, error) {
	user, err := core.GetUser(info.Id)
	if err != nil {
		return nil, err
	}
	if user.Id() == 0 {
		user.Info = info
		user.CreatedAt = time.Now()
	}
	if err := user.Save(); err != nil {
		return nil, err
	}
	return user, nil
}

func (core *Core) GetUsers() ([]*User, error) {
	items, err := os.ReadDir(path.Join(core.ConfigFile.Config.BaseDir, `users`))
	if err != nil {
		ErrorLog.Println(err.Error())
		PrometheusErrors.With(prometheus.Labels{`action`: `get_users`}).Inc()
		return nil, err
	}
	users := make([]*User, 0)
	for _, item := range items {
		if item.IsDir() {
			continue
		}
		user, err := NewUser(path.Join(core.ConfigFile.Config.BaseDir, `users`, item.Name()))
		if err != nil {
			continue
		}
		users = append(users, user)
	}
	return users, err
}

func (core *Core) SendItem(content, url string, categories []string) {
	users, err := core.GetUsers()
	if err != nil {
		return
	}
	for _, user := range users {
		if user.IsInExcludedCategories(categories...) {
			DebugLog.Printf("skip for @%s\n", user.Info.Username)
			continue
		}
		DebugLog.Printf("send to @%s\n", user.Info.Username)
		PrometheusSendItems.With(prometheus.Labels{`username`: user.Info.Username}).Inc()
		message := telegram.SendMessageIntWithoutReplyMarkup{}
		message.ChatId = user.Id()
		message.Text = fmt.Sprintf("%v\n%s\n\n%s", categories, content, url)
		if err := core.TelegramApi.RequestWrapper(``, message, func() { core.RemoveUser(user.Id()) }); err != nil {
			PrometheusErrors.With(prometheus.Labels{`action`: `telegram_request`}).Inc()
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (core *Core) TelegramHttpHandler(w http.ResponseWriter, r *http.Request) {
	DebugLog.Printf("%s : %s : %s : %s\n", r.Header.Get(`X-Real-IP`), r.Method, r.RequestURI, r.UserAgent())
	if r.Method != http.MethodPost {
		ErrorLog.Println(r.Method, r.Header, r.RequestURI)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		ErrorLog.Println(err.Error())
		PrometheusErrors.With(prometheus.Labels{`action`: `telegram_handler`}).Inc()
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	update, err := telegram.UnmarshalUpdate(body)
	if err != nil {
		ErrorLog.Println(string(body), err.Error())
		PrometheusErrors.With(prometheus.Labels{`action`: `telegram_handler`}).Inc()
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	go func() {
		if update.IsMessage() {
			core.TelegramMessage(update)
			return
		}
		if update.IsCallbackQuery() {
			core.TelegramCallback(update)
			return
		}
	}()
}

func (core *Core) RemoveUser(id int) error {
	DebugLog.Printf("Removing user %d", id)
	return os.Remove(path.Join(core.ConfigFile.Config.BaseDir, `users`,
		fmt.Sprintf("%d.yml", id)))
}

func (core *Core) GetCategoriesButtons(user *User) [][]telegram.InlineKeyboardButton {
	buttons := make([][]telegram.InlineKeyboardButton, 0)
	for _, category := range core.State.Categories {
		if user.IsInExcludedCategories(category) {
			buttons = append(buttons, []telegram.InlineKeyboardButton{{
				Text:         fmt.Sprintf("⛔️%s", category),
				CallbackData: fmt.Sprintf("include|%s", category),
			}})
			continue
		}
		buttons = append(buttons, []telegram.InlineKeyboardButton{{
			Text:         fmt.Sprintf("🔶 %s", category),
			CallbackData: fmt.Sprintf("exclude|%s", category),
		}})
	}
	return buttons
}

func (core *Core) TelegramMessage(update telegram.Update) {
	DebugLog.Println(update.Message.From, update.Message.Text)
	switch update.Message.Text {
	case `/start`:
		core.TelegramApi.RequestWrapper(`deleteMessage`, telegram.DeleteMessageInt{
			ChatId:    update.Message.Chat.Id,
			MessageId: update.Message.Id,
		}, nil)
		message := telegram.SendMessageIntWithoutReplyMarkup{}
		message.ChatId = update.Message.From.Id
		message.Text = `Бот находится в стадии разработки`
		if _, err := core.GetOrCreateUser(update.Message.From); err != nil {
			message.Text = fmt.Sprintf("%s\n\nОшибка: %s", message.Text, err.Error())
		}
		if err := core.TelegramApi.RequestWrapper(``, message, nil); err != nil {
			PrometheusErrors.With(prometheus.Labels{`action`: `telegram_request`}).Inc()
		}
	case `/categories`:
		user, err := core.GetOrCreateUser(update.Message.From)
		if err != nil {
			PrometheusErrors.With(prometheus.Labels{`action`: `get_user`}).Inc()
			message := telegram.SendMessageIntWithoutReplyMarkup{}
			message.ChatId = update.Message.From.Id
			message.Text = `Извините, произошла ошибка.`
			if err := core.TelegramApi.RequestWrapper(``, message, nil); err != nil {
				PrometheusErrors.With(prometheus.Labels{`action`: `telegram_request`}).Inc()
			}
		}
		core.TelegramApi.RequestWrapper(`deleteMessage`, telegram.DeleteMessageInt{
			ChatId:    update.Message.Chat.Id,
			MessageId: update.Message.Id,
		}, nil)
		payload := telegram.SendMessageIntWithInlineKeyboardMarkup{
			ReplyMarkup: telegram.InlineKeyboardMarkup{
				InlineKeyboard: core.GetCategoriesButtons(user),
			},
		}
		payload.Text = `Категории:`
		payload.ChatId = update.Message.From.Id
		if err := core.TelegramApi.RequestWrapper(``, payload, nil); err != nil {
			PrometheusErrors.With(prometheus.Labels{`action`: `telegram_request`}).Inc()
		}
	}
}

func (core *Core) TelegramCallback(update telegram.Update) {
	user, err := core.GetUser(update.CallbackQuery.Message.Chat.Id)
	if err != nil {
		ErrorLog.Println(err.Error())
		PrometheusErrors.With(prometheus.Labels{`action`: `get_user`}).Inc()
		return
	}
	command := strings.Split(update.CallbackQuery.Data, `|`)
	switch command[0] {
	case `include`:
		DebugLog.Printf("@%s want to include: %s\n", user.Info.Username, command[1])
		if err := user.RemoveExcludedCategory(command[1]); err != nil {
			ErrorLog.Println(err.Error())
			PrometheusErrors.With(prometheus.Labels{`action`: `include`}).Inc()
			return
		}
	case `exclude`:
		DebugLog.Printf("@%s want to exclude: %s\n", user.Info.Username, command[1])
		if err := user.AddExcludedCategory(command[1]); err != nil {
			ErrorLog.Println(err.Error())
			PrometheusErrors.With(prometheus.Labels{`action`: `exclude`}).Inc()
			return
		}
	}
	payload := telegram.EditMessageIntInlineKeyboardMarkup{
		ChatId:    update.CallbackQuery.Message.Chat.Id,
		MessageId: update.CallbackQuery.Message.Id,
		ReplyMarkup: telegram.InlineKeyboardMarkup{
			InlineKeyboard: core.GetCategoriesButtons(user),
		},
	}
	if err := core.TelegramApi.RequestWrapper(`editMessageReplyMarkup`, payload, nil); err != nil {
		PrometheusErrors.With(prometheus.Labels{`action`: `telegram_request`}).Inc()
	}
}

func NewCore(configFile *ConfigFile, telegramApi *telegram.Api, state *State) (*Core, error) {
	core := Core{
		ConfigFile:  configFile,
		TelegramApi: telegramApi,
		State:       state,
	}
	if err := os.MkdirAll(path.Join(configFile.Config.BaseDir, `users`), 0744); err != nil {
		ErrorLog.Println(err.Error())
		return nil, err
	}
	return &core, nil
}
