package main

import "strings"

// diveIntoCategories
//
// Если в категории идет перечисление категорий через запятую, то разбираем это на отдельные категории
func diveIntoCategories(categories []string) []string {
	newCategories := make([]string, 0)
	for _, category := range categories {
		for _, c := range strings.Split(category, `, `) {
			if c == `` {
				continue
			}
			newCategories = append(newCategories, c)
		}
	}
	return newCategories
}
