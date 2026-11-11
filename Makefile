.PHONY: reference
reference:
	UPDATE_REFERENCE=1 go test -run '^TestReference$$' .
