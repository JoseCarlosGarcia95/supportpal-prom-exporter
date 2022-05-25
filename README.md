# supportpal-prom-exporter

## How to use

Define the following environment variables:

- API_BASE_PATH: The base path of the API.
- API_TOKEN: The token to use for authentication.

## Example metrics

````
supportpal_ticket_updated{client="one-org",priority="low",status="open",subject="One Subject",user="one-user"} 1.653431774e+09
supportpal_ticket_created{client="one-org",priority="low",status="open",subject="One Subject",user="one-user"} 1.653431774e+09
````
