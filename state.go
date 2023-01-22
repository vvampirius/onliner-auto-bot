package main

import (
	"gopkg.in/yaml.v2"
	"os"
	"time"
)

type State struct {
	path       string
	LastDate   time.Time `yaml:"last_date"`
	Categories []string
}

func (state *State) Load() error {
	f, err := os.Open(state.path)
	if err != nil {
		ErrorLog.Println(err.Error())
		return err
	}
	defer f.Close()
	decoder := yaml.NewDecoder(f)
	if err := decoder.Decode(state); err != nil {
		ErrorLog.Println(err.Error())
		return err
	}
	return nil
}

func (state *State) Save() error {
	f, err := os.OpenFile(state.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		ErrorLog.Println(err.Error())
		return err
	}
	defer f.Close()
	encoder := yaml.NewEncoder(f)
	if err := encoder.Encode(*state); err != nil {
		ErrorLog.Println(err.Error())
		return err
	}
	return nil
}

func (state *State) IsInCategories(category string) bool {
	for _, v := range state.Categories {
		if v == category {
			return true
		}
	}
	return false
}

func (state *State) AddCategory(categories ...string) int {
	added := 0
	for _, category := range categories {
		if state.IsInCategories(category) {
			continue
		}
		state.Categories = append(state.Categories, category)
		added++
	}
	return added
}

func NewState(path string) (*State, error) {
	state := State{
		path: path,
	}
	if err := state.Load(); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		state.Categories = make([]string, 0)
	}
	return &state, nil
}
