package mode

import "strings"

type Mode string

const (
	Off        Mode = "off"
	On         Mode = "on"
	AssistList Mode = "assist_list"
	FilterList Mode = "filter_list"
)

func Parse(v string) (Mode, bool) {
	m := Mode(strings.ToLower(strings.TrimSpace(v)))
	switch m {
	case Off, On, AssistList, FilterList:
		return m, true
	default:
		return Off, false
	}
}

func (m Mode) Active() bool {
	return m == On || m == AssistList || m == FilterList
}
