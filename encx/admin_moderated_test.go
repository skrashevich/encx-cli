package encx

import "testing"

// The editor renders the setting as a radio pair; reading it as a checkbox
// reported every game as moderated, which made a successful "автоприём заявок"
// write look like a silent failure.
func TestParseCheckedRadioBoolReadsTheSelectedButton(t *testing.T) {
	const automatic = `<input type="radio" id="IsModeratedYes" name="IsModerated" value="true"/>` +
		`<input type="radio" id="IsModeratedNo" name="IsModerated" value="false" checked="checked"/>`
	const moderated = `<input type="radio" id="IsModeratedYes" name="IsModerated" value="true" checked="checked"/>` +
		`<input type="radio" id="IsModeratedNo" name="IsModerated" value="false"/>`

	if parseCheckedRadioBool(automatic, "IsModerated") {
		t.Error("value=\"false\" is the checked button, so the game is not moderated")
	}
	if !parseCheckedRadioBool(moderated, "IsModerated") {
		t.Error("value=\"true\" is the checked button, so the game is moderated")
	}
	if parseCheckedRadioBool(automatic, "Missing") {
		t.Error("an absent group must read false")
	}
	// The old reader keyed on the name alone and so could not tell these apart.
	if !parseCheckedInputs(automatic)["IsModerated"] || !parseCheckedInputs(moderated)["IsModerated"] {
		t.Error("guard: parseCheckedInputs is expected to report true for both")
	}
}

// Some pages spell the setting as a bare checkbox, where being checked is the
// whole answer and there is no value to read.
func TestParseCheckedRadioBoolHandlesValuelessCheckbox(t *testing.T) {
	if !parseCheckedRadioBool(`<input type="checkbox" name="IsModerated" checked>`, "IsModerated") {
		t.Error("a checked checkbox without a value must read true")
	}
	if parseCheckedRadioBool(`<input type="checkbox" name="IsModerated">`, "IsModerated") {
		t.Error("an unchecked checkbox must read false")
	}
}
