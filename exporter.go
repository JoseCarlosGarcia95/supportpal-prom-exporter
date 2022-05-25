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
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	DeletedAt   int64  `json:"deleted_at"`
	OperatorURL string `json:"operator_url"`
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
	url := "/api/ticket/ticket?start=" + strconv.Itoa(start) + "&limit=" + strconv.Itoa(limit)
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

// fetchAllTickets is a helper function to fetch all tickets and return a slice of Ticket
func fetchAllTickets() ([]*Ticket, error) {
	var tickets []*Ticket
	start := 0
	limit := 2000

	total := 0

	for {
		if total-start < limit && total > 0 {
			limit = total - start
		}

		ticketsResponse, err := listTickets(start, limit)

		total = ticketsResponse.Count

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

var (
	supportPalTicketUpdated = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "supportpal_ticket_updated",
		Help: "Last time a ticket was updated",
	}, []string{"client", "status", "priority", "user", "subject"})

	supportPalTicketCreated = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "supportpal_ticket_created",
		Help: "Last time a ticket was created",
	}, []string{"client", "status", "priority", "user", "subject"})

	supportPalTicketDeleted = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "supportpal_ticket_deleted",
		Help: "Last time a ticket was deleted",
	}, []string{"client", "status", "priority", "user", "subject"})
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

		for _, ticket := range tickets {
			orgName := "no-org"

			// If ticket has more than one year since last update, ignore it
			if time.Now().Unix()-ticket.UpdatedAt > 31536000 {
				continue
			}

			if ticket.User.OrganizationID != 0 {
				org, err := getOrganization(ticket.User.OrganizationID)

				if err != nil {
					log.Println(err)
					continue
				}

				if org.Data != nil {
					orgName = org.Data.Name
				}

				orgName = strings.Replace(orgName, " ", "", -1)
				orgName = strings.ToLower(orgName)
			}

			if ticket.DeletedAt != 0 {
				supportPalTicketDeleted.With(prometheus.Labels{
					"organization": orgName,
					"status":       strings.ToLower(ticket.Status.Name),
					"priority":     strings.ToLower(ticket.Priority.Name),
					"user":         strings.ToLower(ticket.User.FormattedName),
					"subject":      ticket.Subject,
				}).Set(float64(ticket.DeletedAt))
			}

			if ticket.CreatedAt != 0 {
				supportPalTicketCreated.With(prometheus.Labels{
					"organization": orgName,
					"status":       strings.ToLower(ticket.Status.Name),
					"priority":     strings.ToLower(ticket.Priority.Name),
					"user":         strings.ToLower(ticket.User.FormattedName),
					"subject":      ticket.Subject,
				}).Set(float64(ticket.CreatedAt))
			}

			if ticket.UpdatedAt != 0 {
				supportPalTicketUpdated.With(prometheus.Labels{
					"organization": orgName,
					"status":       strings.ToLower(ticket.Status.Name),
					"priority":     strings.ToLower(ticket.Priority.Name),
					"user":         strings.ToLower(ticket.User.FormattedName),
					"subject":      ticket.Subject,
				}).Set(float64(ticket.UpdatedAt))
			} else {
				supportPalTicketUpdated.With(prometheus.Labels{
					"organization": orgName,
					"status":       strings.ToLower(ticket.Status.Name),
					"priority":     strings.ToLower(ticket.Priority.Name),
					"user":         strings.ToLower(ticket.User.FormattedName),
					"subject":      ticket.Subject,
				}).Set(float64(ticket.CreatedAt))
			}
		}

		time.Sleep(time.Duration(60) * time.Second)
	}
}
func main() {

	go collectMetrics()
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":20000", nil)
}
