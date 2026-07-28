package db

import (
	dhtcclient "dhtc/dhtc-client"
	"fmt"
	"unicode"
)

func ContainsChineseContent(md dhtcclient.Metadata) bool {
	if containsHan(md.Name) {
		return true
	}

	for _, file := range md.Files {
		if containsHan(file.Path) {
			return true
		}
	}

	return false
}

func ContainsChineseMetaData(md MetaData) bool {
	if containsHan(md.Name) {
		return true
	}

	for _, file := range md.Files {
		switch v := file.(type) {
		case map[string]any:
			if containsHan(fmt.Sprint(v["path"])) || containsHan(fmt.Sprint(v["Path"])) {
				return true
			}
		default:
			if containsHan(fmt.Sprint(v)) {
				return true
			}
		}
	}

	return false
}

func containsHan(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
