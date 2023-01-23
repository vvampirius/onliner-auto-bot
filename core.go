package main

import (
	"fmt"
	"github.com/mmcdole/gofeed"
	"github.com/vvampirius/mygolibs/telegram"
	"io"
	"net/http"
	"os"
	"path"
	"time"
)

type Core struct {
	ConfigFile  *ConfigFile
	TelegramApi *telegram.Api
	State       *State
}

func (core *Core) TelegramSend(method string, payload interface{}, onBlocked func()) {
	if method == `` {
		method = `sendMessage`
	}
	statusCode, response, err := core.TelegramApi.Request(`sendMessage`, payload)
	if err != nil {
		ErrorLog.Println(err.Error())
		// TODO: prometheus counter
		return
	}
	if statusCode != 200 {
		if statusCode == 403 && response.Description == `Forbidden: bot was blocked by the user` && onBlocked != nil {
			onBlocked()
		}
		ErrorLog.Println(statusCode, response.Description)
		// TODO: prometheus counter
		return
	}
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
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	go func() {
		var newLastDate time.Time
		newItems := 0
		for _, item := range feed.Items {
			if item.PublishedParsed == nil {
				ErrorLog.Println(`Can't parse`, item.PublishedParsed)
				continue
			}
			if item.PublishedParsed.After(core.State.LastDate) {
				newItems++
				if item.PublishedParsed.After(newLastDate) {
					newLastDate = *item.PublishedParsed
				}
				core.State.AddCategory(item.Categories...)
				core.SendItem(item.Title, item.Link, item.Categories)
			}
		}
		DebugLog.Printf("Got %d new items\n", newItems)
		if !newLastDate.IsZero() {
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
	DebugLog.Println(content, url, categories)
	users, err := core.GetUsers()
	if err != nil {
		return
	}
	for _, user := range users {
		DebugLog.Println(user.Info)
		if user.IsInExcludedCategories(categories...) {
			DebugLog.Println(`Excluding`)
			continue
		}
		message := telegram.SendMessageIntWithoutReplyMarkup{}
		message.ChatId = user.Id()
		message.Text = fmt.Sprintf("%v\n%s\n\n%s", categories, content, url)
		core.TelegramSend(``, message, func() { core.RemoveUser(user.Id()) })
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
		return
	}
	update, err := telegram.UnmarshalUpdate(body)
	if err != nil {
		ErrorLog.Println(string(body), err.Error())
		//core.Prometheus.Errors.With(prometheus.Labels{`situation`: `unmarshal_update`}).Inc()
		return
	}
	if update.IsMessage() {
		go core.TelegramMessage(update)
		return
	}
	//if update.IsCallbackQuery() {
	//go core.TelegramCallback(update)
	//return
	//}
}

func (core *Core) RemoveUser(id int) error {
	DebugLog.Printf("Removing user %d", id)
	return os.Remove(path.Join(core.ConfigFile.Config.BaseDir, `users`,
		fmt.Sprintf("%d.yml", id)))
}

func (core *Core) TelegramMessage(update telegram.Update) {
	DebugLog.Println(update.Message.Text)
	DebugLog.Println(update.Message.From)
	switch update.Message.Text {
	case `/start`:
		message := telegram.SendMessageIntWithoutReplyMarkup{}
		message.ChatId = update.Message.From.Id
		message.Text = `Бот находится в стадии разработки`
		if _, err := core.GetOrCreateUser(update.Message.From); err != nil {
			message.Text = fmt.Sprintf("%s\n\nОшибка: %s", message.Text, err.Error())
		}
		core.TelegramSend(``, message, nil)
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
