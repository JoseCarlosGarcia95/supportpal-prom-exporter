package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gosimple/slug"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var organizationCache = make(map[int]Organization)

// requestAPI is a helper function to make an API request that accepts method, url, and body
func requestAPI(method, url string, body []byte) ([]byte, error) {
	baseURL := os.Getenv("API_BASE_PATH")

	if baseURL[len(baseURL)-1:] == "/" {
		baseURL = baseURL[:len(baseURL)-1]
	}

	url = baseURL + url
	req, err := http.NewRequest(method, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(os.Getenv("API_TOKEN"), "X")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)
}

// Ticket represents the response from the API
type Ticket struct {
	ID      int    `json:"id"`
	Subject string `json:"subject"`
	Status  struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"status"`
	Priority struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"priority"`
	User struct {
		FormattedName  string `json:"formatted_name"`
		OrganizationID int    `json:"organisation_id"`
	}
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
	DeletedAt    int64  `json:"deleted_at"`
	ResolvedTime int64  `json:"resolved_time"`
	OperatorURL  string `json:"operator_url"`
	FrontendURL  string `json:"frontend_url"`
	CustomFields []*struct {
		ID      int    `json:"id"`
		FieldID int    `json:"field_id"`
		Value   string `json:"value"`
	} `json:"customfields"`
}

// respListTickets represents the response body for listing tickets
type respListTickets struct {
	Status  string    `json:"status"`
	Message string    `json:"message"`
	Count   int       `json:"count"`
	Data    []*Ticket `json:"data"`
}

// listTickets is a helper function to list tickets with start and limit
func listTickets(start, limit int) (*respListTickets, error) {
	url := "/api/ticket/ticket?order_direction=desc&start=" + strconv.Itoa(start) + "&limit=" + strconv.Itoa(limit)
	resp, err := requestAPI("GET", url, nil)

	if err != nil {
		return nil, err
	}

	var tickets respListTickets
	err = json.Unmarshal(resp, &tickets)
	if err != nil {
		return nil, err
	}

	return &tickets, nil
}

// Organization represents an organization
type Organization struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// respGetOrganization represents the response body for getting an organization
type respGetOrganization struct {
	Status  string        `json:"status"`
	Message string        `json:"message"`
	Data    *Organization `json:"data"`
}

// getOrganization is a helper function to get an organization
func getOrganization(id int) (*respGetOrganization, error) {
	if ok := organizationCache[id]; ok != (Organization{}) {
		return &respGetOrganization{
			Status:  "success",
			Message: "",
			Data:    &ok,
		}, nil
	}

	url := "/api/user/organisation/" + strconv.Itoa(id)
	resp, err := requestAPI("GET", url, nil)

	if err != nil {
		return nil, err
	}

	var organization respGetOrganization
	err = json.Unmarshal(resp, &organization)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	organizationCache[id] = *organization.Data

	return &organization, nil
}

// respGetCustomField represents the response body for getting a custom field
type respGetCustomField struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Type    int    `json:"type"`
		Options []struct {
			ID    int    `json:"id"`
			Value string `json:"value"`
		} `json:"options"`
	} `json:"data"`
}

var customFieldCache = make(map[int]*respGetCustomField)

// getCustomField is a helper function to get a custom field
func getCustomField(id int) (*respGetCustomField, error) {
	if ok := customFieldCache[id]; ok != nil {
		return ok, nil
	}
	url := "/api/ticket/customfield/" + strconv.Itoa(id)
	resp, err := requestAPI("GET", url, nil)

	if err != nil {
		return nil, err
	}

	var customField respGetCustomField
	err = json.Unmarshal(resp, &customField)

	if err == nil {
		customFieldCache[id] = &customField
	}

	return &customField, nil
}

// fetchAllTickets is a helper function to fetch all tickets and return a slice of Ticket
func fetchAllTickets() ([]*Ticket, error) {
	var tickets []*Ticket
	start := 0
	limit := 2000
	for {
		ticketsResponse, err := listTickets(start, limit)

		if err != nil {
			return nil, err
		}

		tickets = append(tickets, ticketsResponse.Data...)

		if ticketsResponse.Count <= len(tickets) {
			break
		}

		start += limit
	}

	return tickets, nil
}

// CommonLabels is a map of labels that are common to all tickets
var CommonLabels = []string{"client", "status", "priority", "user", "subject", "ticket_url", "frontend_url"}

var (
	supportPalTicketUpdated  = &prometheus.GaugeVec{}
	supportPalTicketCreated  = &prometheus.GaugeVec{}
	supportPalTicketDeleted  = &prometheus.GaugeVec{}
	supportPalTicketResolved = &prometheus.GaugeVec{}
	globaLabels              = []string{}
)

func collectMetrics() {
	for {
		log.Println("Collecting metrics...")

		log.Println("List all tickets...")

		tickets, err := fetchAllTickets()

		if err != nil {
			log.Println(err)
			continue
		}

		log.Println("List all tickets...done")

		log.Println("Cleaning old metrics...")

		supportPalTicketCreated.Reset()
		supportPalTicketUpdated.Reset()
		supportPalTicketDeleted.Reset()

		for _, ticket := range tickets {
			// ignore tickets oldes than 1 year
			if time.Unix(ticket.CreatedAt, 0).AddDate(1, 0, 0).Before(time.Now()) {
				continue
			}

			labels := prometheus.Labels{
				"status":       strings.ToLower(ticket.Status.Name),
				"priority":     strings.ToLower(ticket.Priority.Name),
				"user":         strings.ToLower(ticket.User.FormattedName),
				"subject":      ticket.Subject,
				"ticket_url":   ticket.OperatorURL,
				"frontend_url": ticket.FrontendURL,
			}

			if ticket.User.OrganizationID != 0 {
				org, err := getOrganization(ticket.User.OrganizationID)

				if err != nil {
					log.Println(err)
					continue
				}

				orgName := ""
				if org.Data != nil {
					orgName = org.Data.Name
				}

				orgName = strings.Replace(orgName, " ", "", -1)
				orgName = strings.ToLower(orgName)

				labels["client"] = orgName
			}

			for _, customField := range ticket.CustomFields {
				cField, err := getCustomField(customField.FieldID)

				if err != nil {
					log.Println(err)
					continue
				}

				name := slug.Make(cField.Data.Name)
				name = strings.ReplaceAll(name, "-", "_")
				value := customField.Value

				if cField.Data.Type == 7 {
					for _, option := range cField.Data.Options {
						nVal, _ := strconv.Atoi(value)
						if option.ID == nVal {
							value = slug.Make(option.Value)
							break
						}
					}
				}

				labels[name] = value
			}

			for _, label := range globaLabels {
				if _, ok := labels[label]; !ok {
					labels[label] = ""
				}
			}

			if ticket.DeletedAt != 0 {
				supportPalTicketDeleted.With(labels).Set(float64(ticket.DeletedAt))
			}

			if ticket.CreatedAt != 0 {
				supportPalTicketCreated.With(labels).Set(float64(ticket.CreatedAt))
			}

			if ticket.UpdatedAt != 0 {
				supportPalTicketUpdated.With(labels).Set(float64(ticket.UpdatedAt))
			} else {
				supportPalTicketUpdated.With(labels).Set(float64(ticket.CreatedAt))
			}

			if ticket.ResolvedTime != 0 {
				supportPalTicketResolved.With(labels).Set(float64(ticket.ResolvedTime))
			}
		}

		time.Sleep(time.Duration(60) * time.Second)
	}
}

func initializeMetrics() {
	log.Println("Initializing metrics...")
	tickets, err := fetchAllTickets()

	if err != nil {
		log.Fatal(err)
	}

	// Copy commonLabels to labels
	globaLabels = make([]string, len(CommonLabels))
	for k, v := range CommonLabels {
		globaLabels[k] = v
	}

	for _, ticket := range tickets {
		for _, customField := range ticket.CustomFields {
			cField, err := getCustomField(customField.FieldID)

			if err != nil {
				log.Println(err)
				continue
			}

			name := slug.Make(cField.Data.Name)
			name = strings.ReplaceAll(name, "-", "_")
			found := false

			for _, v := range globaLabels {
				if v == name {
					found = true
					break
				}
			}

			if !found {
				globaLabels = append(globaLabels, name)
			}
		}
	}

	// Create metrics
	supportPalTicketCreated = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "supportpal_ticket_created",
		Help: "Last time a ticket was created",
	}, globaLabels)

	supportPalTicketUpdated = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "supportpal_ticket_updated",
		Help: "Last time a ticket was updated",
	}, globaLabels)

	supportPalTicketDeleted = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "supportpal_ticket_deleted",
		Help: "Last time a ticket was deleted",
	}, globaLabels)

	supportPalTicketResolved = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "supportpal_ticket_resolved",
		Help: "Last time a ticket was resolved",
	}, globaLabels)

	log.Println("Metrics initialized.")
}

func main() {

	initializeMetrics()
	go collectMetrics()
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":20000", nil)
}
