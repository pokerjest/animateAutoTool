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
