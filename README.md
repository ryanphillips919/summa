# Summa

A collaborative, cloud-native accounting software system.

## Team
- David Kendall
- Ryan Phillips
- Graham Taggart
- Sawyer Evans

## Status
CS 4000 Capstone project, Fall 2026.

## Installation for dependencies 
```sh
# use this on to install templ onto your path
go install github.com/a-h/templ/cmd/templ@latest
# or install locally to the project if you don't want templ on your path
go get -tool github.com/a-h/templ/cmd/templ@latest 
# for the actual dependencies that aren't the standard lib
go get -u github.com/starfederation/datastar-go modernc.org/sqlite github.com/go-chi/chi/v5 github.com/nats-io/nats.go@latest github.com/nats-io/nats-server/v2@latest
# sqlc and goose
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/pressly/goose/v3/cmd/goose@latest
```


## Generating and running the code
For the templ code.
```sh
templ generate # whenever you change a templ file you need to run this to apply the changes. templ is compiled into Go code so without it the pages won't update
# otherwise for local development you can use this to watch all the changes and apply in real-time
templ generate --watch --proxy="http://localhost:8080" --cmd="go run ."
```

For the sqlc and goose migrations
```sh
sqlc generate # this generates the sql into Golang code. Whenever you update your SQL commands/queries run sqlc generate.
```

Goose is for our database migrations, and creating the tables.
Whenenver you need to alter a table/create a new table you'll need to run the below command. Always add the -s flag because it
generates the sql file as 00000_some_database_manipulation.sql, and goose migrates the database in lexicographical order.
```sh
goose -s create some_database_manipulation sql
# to run migrations themselves
goose up
# when you need to downgrade
goose down
```

For the server itself for local testing use go run ., you can also use go run main.go if you feel more comfortable that way.
```sh
go run . # basically says run this code at the root directory, so with chi it will start up the server and print the localhost to open the server and see
```

In the CI/CD pipeline there will be some commands that Go comes built in that are really useful for formatting, linting, testing etc.
```sh
go fmt ./.. # format your golang code in all directories
go test . # test all the test files. It's nice because we can have a single test.go file and Go knows how to run the tests, and will exclude them in the build
go build . # build this into a Golang binary for deployment/this is what Docker will build for us for the VM.
```
