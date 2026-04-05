// Package main provides a tool to seed the database with passive skill nodes.
//
// To seed a production database that doesn't expose its database port:
//
//  1. Build a Linux binary from your local machine:
//     env GOOS=linux GOARCH=amd64 go build -o seed_prod ./cmd/tools/seed_passives
//
//  2. Securely copy the binary and the JSON file to your droplet:
//     scp seed_prod passives.json herald@<YOUR_DROPLET_IP>:~
//
//  3. SSH into your droplet and run it (passing the production DSN):
//     ssh herald@<YOUR_DROPLET_IP>
//     DB_DSN="postgres://envoy:YOUR_PROD_PASSWORD@localhost:5432/poe_herald" ./seed_prod passives.json
package main
