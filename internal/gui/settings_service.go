package gui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pixambi/sev1situations/internal/settings"
)

// ThemeKey is the application setting that holds the theme override (6.7). The
// program registers it with the store beside the products' fields (app.AppSettings),
// and the settings page gives it a choice of values.
const ThemeKey = "app.theme"

// themes are the values the theme setting may hold. Empty follows the system.
var themes = map[string]bool{"": true, "light": true, "dark": true}

// SettingsService reads and saves the settings (3.4). It never returns a secret:
// a saved license is shown masked, and can be replaced or cleared but not read back
// (R-SET-3).
type SettingsService struct {
	store *settings.Store
}

// NewSettingsService returns a service over the store.
func NewSettingsService(store *settings.Store) *SettingsService {
	return &SettingsService{store: store}
}

// SettingField is one setting as the page shows it.
type SettingField struct {
	Key    string
	Label  string
	Help   string
	Secret bool
	// Set is true when a value is saved.
	Set bool
	// Display is the saved value, or a mask for a secret, or empty.
	Display string
}

// SettingsView is what the settings page shows.
type SettingsView struct {
	Instructor string
	Fields     []SettingField
}

// Get returns the settings, with secrets masked.
func (s *SettingsService) Get() SettingsView {
	v := s.store.View()
	out := SettingsView{Instructor: v.Instructor, Fields: make([]SettingField, 0, len(v.Fields))}
	for _, f := range v.Fields {
		out.Fields = append(out.Fields, SettingField{Key: f.Key, Label: f.Label, Help: f.Help, Secret: f.Secret, Set: f.Set, Display: f.Display})
	}
	return out
}

// SaveInstructor saves the instructor's name, which the Start form starts with.
func (s *SettingsService) SaveInstructor(name string) (SettingsView, error) {
	if err := s.store.SetInstructor(name); err != nil {
		return s.Get(), settingError(err)
	}
	return s.Get(), nil
}

// SaveField saves one setting. An empty value clears it. A license is replaced by
// saving a new one, and the old one cannot be read back.
func (s *SettingsService) SaveField(key, value string) (SettingsView, error) {
	if key == ThemeKey && !themes[strings.TrimSpace(value)] {
		return s.Get(), notice("The theme can be light, dark, or left to follow the system.")
	}
	if err := s.store.Set(key, value); err != nil {
		return s.Get(), settingError(err)
	}
	return s.Get(), nil
}

// settingError turns the store's errors into sentences. They never contain a value.
func settingError(err error) error {
	switch {
	case errors.Is(err, settings.ErrUnknownField):
		return notice("There is no such setting.")
	case errors.Is(err, settings.ErrInvalidValue):
		return notice(fmt.Sprintf("That value cannot be saved: %s.", reason(err)))
	}
	return fmt.Errorf("save the settings: %w", err)
}

// reason picks the part of a store error after its last colon, which is the rule that
// was broken, for example "longer than 8192 characters".
func reason(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		return msg[i+2:]
	}
	return msg
}
