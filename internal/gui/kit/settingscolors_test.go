package kit

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"
)

func TestSettingsEditorColors(t *testing.T) {
	d, l := DarkModern(), LightModern()
	for _, tc := range []struct {
		name string
		got  ui.Color
		want uint32
	}{
		{"dark settings.headerForeground", d.SettingsHeaderForeground, 0xFFFFFFFF},
		{"dark settings.modifiedItemIndicator", d.SettingsModifiedItemIndicator, 0x66BB8009},
		{"dark settings.dropdownBackground", d.SettingsDropdownBackground, 0xFF313131},
		{"dark settings.dropdownBorder", d.SettingsDropdownBorder, 0xFF3C3C3C},
		{"dark settings.checkboxBackground", d.SettingsCheckboxBackground, 0xFF313131},
		{"dark settings.textInputBackground", d.SettingsTextInputBackground, 0xFF313131},
		{"dark settings.textInputBorder", d.SettingsTextInputBorder, 0xFF3C3C3C},
		{"dark settings.numberInputForeground", d.SettingsNumberInputForeground, 0xFFCCCCCC},
		{"dark settings.focusedRowBorder", d.SettingsFocusedRowBorder, 0xFF0078D4},
		{"dark settings.rowHoverBackground", d.SettingsRowHoverBackground, 0x4D2A2D2E},
		{"dark settings.focusedRowBackground", d.SettingsFocusedRowBackground, 0x992A2D2E},
		{"light settings.headerForeground", l.SettingsHeaderForeground, 0xFF1F1F1F},
		{"light settings.modifiedItemIndicator", l.SettingsModifiedItemIndicator, 0x66BB8009},
		{"light settings.dropdownBackground", l.SettingsDropdownBackground, 0xFFFFFFFF},
		{"light settings.dropdownBorder", l.SettingsDropdownBorder, 0xFFCECECE},
		{"light settings.numberInputBorder", l.SettingsNumberInputBorder, 0xFFCECECE},
		{"light settings.textInputBorder", l.SettingsTextInputBorder, 0xFFCECECE},
	} {
		if tc.got != ui.NewColor(tc.want) {
			t.Errorf("%s = %v, want %#08x", tc.name, tc.got, tc.want)
		}
	}
	if d.SettingsHeaderBorder.Alpha() == 0 || d.SettingsSashBorder != d.SettingsHeaderBorder {
		t.Errorf("header border %v sash border %v", d.SettingsHeaderBorder, d.SettingsSashBorder)
	}
}

func TestKeybindingsTableColors(t *testing.T) {
	d, l := DarkModern(), LightModern()
	for _, tc := range []struct {
		name string
		got  ui.Color
		want uint32
	}{
		{"dark tree.tableColumnsBorder", d.TableColumnsBorder, 0x20CCCCCC},
		{"light tree.tableColumnsBorder", l.TableColumnsBorder, 0x20616161},
		{"dark tree.tableOddRowsBackground", d.TableOddRowsBackground, 0x0ACCCCCC},
		{"light tree.tableOddRowsBackground", l.TableOddRowsBackground, 0x0A3B3B3B},
		{"dark keybindingTable.headerBackground", d.KeybindingTableHeaderBackground, 0x0ACCCCCC},
		{"light keybindingTable.rowsBackground", l.KeybindingTableRowsBackground, 0x0A3B3B3B},
	} {
		if tc.got != ui.NewColor(tc.want) {
			t.Errorf("%s = %v, want %#08x", tc.name, tc.got, tc.want)
		}
	}
}
