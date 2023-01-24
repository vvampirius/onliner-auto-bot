package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/vvampirius/mygolibs/telegram"
	"gopkg.in/yaml.v2"
	"os"
	"time"
)

type User struct {
	path               string
	Info               telegram.User
	CreatedAt          time.Time `yaml:"created_at"`
	ExcludedCategories []string  `yaml:"excluded_categories"`
	IsAdmin            bool      `yaml:"is_admin"`
}

func (user *User) Id() int {
	return user.Info.Id
}

func (user *User) Load() error {
	f, err := os.Open(user.path)
	if err != nil {
		ErrorLog.Println(err.Error())
		if !os.IsNotExist(err) {
			PrometheusErrors.With(prometheus.Labels{`action`: `load`}).Inc()
		}
		return err
	}
	defer f.Close()
	decoder := yaml.NewDecoder(f)
	if err := decoder.Decode(user); err != nil {
		ErrorLog.Println(err.Error())
		PrometheusErrors.With(prometheus.Labels{`action`: `load`}).Inc()
		return err
	}
	return nil
}

func (user *User) Save() error {
	f, err := os.OpenFile(user.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		ErrorLog.Println(err.Error())
		PrometheusErrors.With(prometheus.Labels{`action`: `save`}).Inc()
		return err
	}
	defer f.Close()
	encoder := yaml.NewEncoder(f)
	if err := encoder.Encode(*user); err != nil {
		ErrorLog.Println(err.Error())
		PrometheusErrors.With(prometheus.Labels{`action`: `save`}).Inc()
		return err
	}
	return nil
}

func (user *User) IsInExcludedCategories(categories ...string) bool {
	for _, category := range categories {
		for _, v := range user.ExcludedCategories {
			if v == category {
				return true
			}
		}
	}
	return false
}

// func (user *User) AddCategory(category string) error {
// if user.IsInCategories(category) {
// ErrorLog.Println(`Already in`)
// return errors.New(`Already in`)
// }
// user.ExcludedCategories = append(user.ExcludedCategories, category)
// return user.Save()
// }

func NewUser(path string) (*User, error) {
	user := User{
		path: path,
	}
	if err := user.Load(); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		user.ExcludedCategories = make([]string, 0)
	}
	return &user, nil
}
