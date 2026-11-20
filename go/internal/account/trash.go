package account

func (a Account) Trashed() bool {
	return a.DeletedAt > 0
}

func Live(accounts []Account) []Account {
	out := make([]Account, 0, len(accounts))
	for _, a := range accounts {
		if !a.Trashed() {
			out = append(out, a)
		}
	}
	return out
}

func Trashed(accounts []Account) []Account {
	out := make([]Account, 0, len(accounts))
	for _, a := range accounts {
		if a.Trashed() {
			out = append(out, a)
		}
	}
	return out
}
