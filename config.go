package main

type Config struct {
	Listen   string
	Telegram struct {
		Token   string
		Webhook string
	}
	BaseDir      string `yaml:"base_dir"`
	StartMessage string `yaml:"start_message"`
}
