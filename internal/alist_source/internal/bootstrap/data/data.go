package data

type MockSetting struct {
    Key     string
    Help    string
    Type    string
    Options string
}

func InitData() {}

func InitialSettings() []MockSetting {
    return []MockSetting{}
}
