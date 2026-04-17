package team

import (
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// titleCaser replaces strings.Title (deprecated) for English-tagged title casing.
var titleCaser = cases.Title(language.English)
