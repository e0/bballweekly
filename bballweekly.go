package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/e0/goff"
	"github.com/mrjones/oauth"
)

var consumer *oauth.Consumer
var callbackDomain string
var requestToken *oauth.RequestToken
var client *goff.Client
var tmpl map[string]*template.Template
var gamesPerWeek []map[string]int

func main() {
	setupConfig()
	setGamesPerWeek()
	tmpl = buildTemplates()

	cssHandler := http.FileServer(http.Dir("./css/"))
	http.Handle("/css/", http.StripPrefix("/css/", cssHandler))

	http.HandleFunc("/", mainHandler)
	http.HandleFunc("/yahoo_callback", yahooCallBackHandler)
	http.HandleFunc("/leagues", leaguesHandler)
	http.HandleFunc("/team_overview", teamOverviewHandler)

	http.ListenAndServe(":8080", nil)
}

func setupConfig() {
	configFile, err := os.Open("app_config.json")

	if err != nil {
		log.Fatal(err)
	}

	decoder := json.NewDecoder(configFile)
	configuration := Configuration{}
	if err := decoder.Decode(&configuration); err != nil {
		log.Fatal(err)
	}

	consumer = goff.GetConsumer(configuration.ClientKey, configuration.ClientSecret)
	callbackDomain = configuration.CallbackDomain
}

func setGamesPerWeek() {
	gamesPerWeekFile, err := os.Open("games_per_week_2015.json")

	if err != nil {
		log.Fatal(err)
	}

	decoder := json.NewDecoder(gamesPerWeekFile)
	gamesPerWeek = []map[string]int{}
	if err := decoder.Decode(&gamesPerWeek); err != nil {
		log.Fatal(err)
	}
}

type Configuration struct {
	CallbackDomain string
	ClientKey      string
	ClientSecret   string
}

func buildTemplates() map[string]*template.Template {
	tmpl = make(map[string]*template.Template)
	tmpl["leagues"] = template.Must(template.ParseFiles("views/leagues.html", "views/layout.html"))
	tmpl["team_overview"] = template.Must(template.ParseFiles("views/team_overview.html", "views/layout.html"))
	return tmpl
}

func mainHandler(w http.ResponseWriter, r *http.Request) {
	var url string
	var err error
	requestToken, url, err = consumer.GetRequestTokenAndUrl(callbackDomain + "/yahoo_callback")

	if err != nil {
		log.Fatal(err)
	}

	http.Redirect(w, r, url, http.StatusFound)
}

func yahooCallBackHandler(w http.ResponseWriter, r *http.Request) {
	verificationCode := r.FormValue("oauth_verifier")

	accessToken, err := consumer.AuthorizeToken(requestToken, verificationCode)

	if err != nil {
		log.Fatal(err)
	}

	httpClient, err := consumer.MakeHttpClient(accessToken)

	if err != nil {
		log.Fatal(err)
	}

	client = goff.NewClient(httpClient)

	http.Redirect(w, r, "/leagues", http.StatusFound)
}

func leaguesHandler(w http.ResponseWriter, r *http.Request) {
	fantasyContent, err := client.GetFantasyContent(goff.YahooBaseURL + "/users;use_login=1/games;game_keys=353/leagues/teams")

	if err != nil {
		log.Fatal(err)
	}

	leagues := fantasyContent.Users[0].Games[0].Leagues

	viewName := "leagues"
	p, _ := loadPage(viewName)

	tmpl[viewName].ExecuteTemplate(w, "layout", &LeaguesPage{Page: *p, Leagues: leagues})
}

func teamOverviewHandler(w http.ResponseWriter, r *http.Request) {
	teamKey := r.URL.Query().Get("teamkey")
	currentWeek, _ := strconv.Atoi(r.URL.Query().Get("currentweek"))
	leagueKey := r.URL.Query().Get("leagueKey")

	leagueSettings, err := client.GetLeagueSettings(leagueKey)

	if err != nil {
		log.Println(err)
	}

	weeks := []int{currentWeek}
	matchups, err := client.GetTeamMatchupsForWeeks(teamKey, weeks)

	matchupOverviews := []MatchupOverview{}
	filteredCategories := getFilteredCategories(leagueSettings.StatCategories)

	for _, m := range matchups {
		matchupOverview := MatchupOverview{Matchup: m, FilteredCategories: filteredCategories}

		for i, t := range m.Teams {
			u := goff.YahooBaseURL + "/team/" + t.TeamKey +
				"/stats;type=week;week=" + strconv.Itoa(m.Week)

			teamContent, err := client.GetFantasyContent(u)

			if err != nil {
				log.Fatal(err)
			}

			teamContent.Team.TeamStats.Stats = getFilteredStats(teamContent.Team.TeamStats.Stats, filteredCategories)

			teamPlayers, err := client.GetTeamRoster(t.TeamKey, m.Week)

			if err != nil {
				log.Fatal(err)
			}

			for j, player := range teamPlayers {
				teamPlayers[j].EditorialTeamAbbr = strings.ToUpper(player.EditorialTeamAbbr)
			}
			teamContent.Team.Players = teamPlayers

			if i == 0 {
				matchupOverview.Team1 = teamContent.Team
			} else {
				matchupOverview.Team2 = teamContent.Team
			}

			matchupOverview.GamesThisWeek = gamesPerWeek[m.Week-1]

			if i == 0 {
				matchupOverview.Team1ProjectedStats = matchupOverview.CalculateProjectedStats(teamPlayers)
			} else {
				matchupOverview.Team2ProjectedStats = matchupOverview.CalculateProjectedStats(teamPlayers)
			}
		}

		matchupOverviews = append(matchupOverviews, matchupOverview)
	}

	if err != nil {
		log.Fatal(err)
	}

	viewName := "team_overview"
	p, _ := loadPage(viewName)

	pageData := &TeamOverviewPage{
		Page:             *p,
		MatchupOverviews: matchupOverviews,
	}

	//	log.Printf("%+v\n", pageData.FilteredCategories)

	tmpl[viewName].ExecuteTemplate(w, "layout", pageData)
}

func loadPage(contentView string) (*Page, error) {
	filename := "views/" + contentView + ".html"
	body, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return &Page{Body: body}, nil
}

func loadViews(contentView string) (*template.Template, error) {
	return template.ParseFiles("views/layout.html", "views/"+contentView+".html")
}

func getFilteredCategories(categories []goff.Stat) []goff.Stat {
	filteredCategories := []goff.Stat{}

	for _, cat := range categories {
		if !cat.IsOnlyDisplayStat {
			filteredCategories = append(filteredCategories, cat)
		}
	}

	return filteredCategories
}

func getFilteredStats(stats []goff.Stat, categories []goff.Stat) []goff.Stat {
	filteredStats := []goff.Stat{}

	for _, stat := range stats {
		for _, cat := range categories {
			if stat.StatId == cat.StatId {
				filteredStats = append(filteredStats, stat)
			}
		}
	}

	return filteredStats
}

type Page struct {
	Body []byte
}

type LeaguesPage struct {
	Page
	Leagues []goff.League
}

type TeamOverviewPage struct {
	Page
	MatchupOverviews []MatchupOverview
}

type MatchupOverview struct {
	Matchup             goff.Matchup
	FilteredCategories  []goff.Stat
	Team1               goff.Team
	Team2               goff.Team
	GamesThisWeek       map[string]int
	Team1ProjectedStats ProjectedTeamStats
	Team2ProjectedStats ProjectedTeamStats
}

func (mo MatchupOverview) CalculateProjectedStats(players []goff.Player) ProjectedTeamStats {
	playerKeys := ""
	for index, player := range players {
		if index != 0 {
			playerKeys += ","
		}
		playerKeys += player.PlayerKey
	}

	content, err := client.GetFantasyContent(
		fmt.Sprintf("%s/players;player_keys=%s/stats",
			goff.YahooBaseURL,
			playerKeys))

	if err != nil {
		log.Fatal(err)
	}

	projectedTeamStats := ProjectedTeamStats{PlayerStats: map[string]ProjectedPlayerStats{}}

	for _, player := range content.Players {
		projectedPlayerStats := ProjectedPlayerStats{}

		var gamesPlayed, fga, fgm, fta, ftm, threes, pts, reb, ast, st, blk, to float64
		for _, s := range player.PlayerStats.Stats {
			sValue, _ := strconv.ParseFloat(s.Value, 64)

			switch s.StatId {
			case 0:
				gamesPlayed = sValue
			case 3:
				fga = sValue
			case 4:
				fgm = sValue
			case 6:
				fta = sValue
			case 7:
				ftm = sValue
			case 10:
				threes = sValue
			case 12:
				pts = sValue
			case 15:
				reb = sValue
			case 16:
				ast = sValue
			case 17:
				st = sValue
			case 18:
				blk = sValue
			case 19:
				to = sValue
			}
		}

		numberOfGames := float64(mo.GamesThisWeek[strings.ToUpper(player.EditorialTeamAbbr)])
		projectedPlayerStats.FGA = roundToTwoDecimals(fga / gamesPlayed * numberOfGames)
		projectedPlayerStats.FGM = roundToTwoDecimals(fgm / gamesPlayed * numberOfGames)
		projectedPlayerStats.FGP = roundToTwoDecimals(fgm / fga)
		projectedPlayerStats.FTA = roundToTwoDecimals(fta / gamesPlayed * numberOfGames)
		projectedPlayerStats.FTM = roundToTwoDecimals(ftm / gamesPlayed * numberOfGames)
		projectedPlayerStats.FTP = roundToTwoDecimals(ftm / fta)
		projectedPlayerStats.Threes = roundToTwoDecimals(threes / gamesPlayed * numberOfGames)
		projectedPlayerStats.PTS = roundToTwoDecimals(pts / gamesPlayed * numberOfGames)
		projectedPlayerStats.REB = roundToTwoDecimals(reb / gamesPlayed * numberOfGames)
		projectedPlayerStats.AST = roundToTwoDecimals(ast / gamesPlayed * numberOfGames)
		projectedPlayerStats.ST = roundToTwoDecimals(st / gamesPlayed * numberOfGames)
		projectedPlayerStats.BLK = roundToTwoDecimals(blk / gamesPlayed * numberOfGames)
		projectedPlayerStats.TO = roundToTwoDecimals(to / gamesPlayed * numberOfGames)

		projectedTeamStats.PlayerStats[player.PlayerKey] = projectedPlayerStats
	}

	return projectedTeamStats
}

func roundToTwoDecimals(input float64) float64 {
	return float64(int(input*100)) / 100
}

type ProjectedTeamStats struct {
	PlayerStats map[string]ProjectedPlayerStats
	FGP         string
	FTP         string
	PTS         float64
	Threes      float64
	REB         float64
	AST         float64
	ST          float64
	BLK         float64
	TO          float64
}

//func (ts ProjectedTeamStats) CalculateTeamStats(gamesThisWeek map[string]int) {

//}

type ProjectedPlayerStats struct {
	FGP    float64
	FTP    float64
	FGA    float64
	FGM    float64
	FTA    float64
	FTM    float64
	Threes float64
	PTS    float64
	REB    float64
	AST    float64
	ST     float64
	BLK    float64
	TO     float64
}
