package main

import (
	"encoding/json"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/e0/goff"
	"github.com/mrjones/oauth"
)

var consumer *oauth.Consumer
var callbackDomain string
var requestToken *oauth.RequestToken
var client *goff.Client
var tmpl map[string]*template.Template

func main() {
	setupConfig()
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

			if i == 0 {
				matchupOverview.Team1 = teamContent.Team
			} else {
				matchupOverview.Team2 = teamContent.Team
			}
		}

		matchupOverviews = append(matchupOverviews, matchupOverview)
	}

	if err != nil {
		log.Fatal(err)
	}

	viewName := "team_overview"
	p, _ := loadPage(viewName)
	
	pageData :=  &TeamOverviewPage{
		Page: *p,
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

func getFilteredCategories(categories []goff.Stat) ([]goff.Stat) {
	filteredCategories := []goff.Stat{}
	
	for _, cat := range categories {
		if (!cat.IsOnlyDisplayStat) {
			filteredCategories = append(filteredCategories, cat)
		}
	}
	
	return filteredCategories
}

func getFilteredStats(stats []goff.Stat, categories []goff.Stat) ([]goff.Stat) {
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
	Matchup goff.Matchup
	FilteredCategories []goff.Stat
	Team1   goff.Team
	Team2   goff.Team
}
