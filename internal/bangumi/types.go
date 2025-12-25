package bangumi

type OauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refresh_token"`
	UserID       int    `json:"user_id"`
}

type UserGroup struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type UserProfile struct {
	ID       int    `json:"id"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   struct {
		Large  string `json:"large"`
		Medium string `json:"medium"`
		Small  string `json:"small"`
	} `json:"avatar"`
	Sign      string `json:"sign"`
	UserGroup int    `json:"user_group"`
}

type Images struct {
	Large  string `json:"large"`
	Common string `json:"common"`
	Medium string `json:"medium"`
	Small  string `json:"small"`
	Grid   string `json:"grid"`
}

type Subject struct {
	ID      int    `json:"id"`
	Type    int    `json:"type"`
	Name    string `json:"name"`
	NameCN  string `json:"name_cn"`
	Summary string `json:"summary"`
	Images  Images `json:"images"`
	Rating  struct {
		Total    int            `json:"total"`
		CountMap map[string]int `json:"count"`
		Score    float64        `json:"score"`
	} `json:"rating"`
	Tags []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"tags"`
	Date string `json:"date"`
	Eps  int    `json:"eps"`
}
