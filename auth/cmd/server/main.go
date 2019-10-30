package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"

	"github.com/google/uuid"

	"github.com/Evertras/events-demo/auth/lib/auth"
	"github.com/Evertras/events-demo/auth/lib/authdb"
	"github.com/Evertras/events-demo/auth/lib/eventprocessor"
	"github.com/Evertras/events-demo/auth/lib/server"
	"github.com/Evertras/events-demo/auth/lib/stream"
	"github.com/Evertras/events-demo/auth/lib/token"
)

const headerAuthToken = "X-Auth-Token"
const headerUserID = "X-User-ID"
const addr = "0.0.0.0:13041"

const kafkaBrokers = "kafka-cp-kafka-headless:9092"

func main() {
	ctx := context.Background()

	db := initDb(ctx)

	randomID := uuid.New().String()
	consumerGroupID, err := db.GetSharedValue(ctx, "auth.consumerGroupID", randomID)

	if err != nil {
		log.Fatal("Failed getting consumer group ID:", err)
	}

	log.Println("Using consumer group ID", consumerGroupID)

	err = initSignKey(ctx, db)

	if err != nil {
		log.Fatal("Failed to initialize token sign key:", err)
	}

	writer, err := stream.NewKafkaStreamWriter([]string{kafkaBrokers})

	if err != nil {
		log.Fatal("Failed to initialize stream writer:", err)
	}

	reader, err := stream.NewKafkaStreamReader([]string{kafkaBrokers}, consumerGroupID)

	if err != nil {
		log.Fatal("Failed to initialize stream reader:", err)
	}

	a, err := auth.New(db, writer)

	if err != nil {
		log.Fatal("Failed to create auth:", err)
	}

	server, err := server.New(addr, a)

	if err != nil {
		log.Fatal("Failed to create server:", err)
	}

	processor, err := eventprocessor.New(db, reader)

	if err != nil {
		log.Fatal("Failed to create processor:", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()

	go func() {
		err := reader.Listen(ctx)

		if err != nil {
			log.Fatalln("Error listening:", err)
		}
	}()

	go func() {
		err := processor.Run(ctx)
		log.Println("Processor finished")
		if err != nil {
			log.Fatalln("Error processing:", err)
		}
	}()

	log.Println("Serving", addr)

	log.Fatal(server.ListenAndServe())
}

func initDb(ctx context.Context) authdb.Db {
	db, err := authdb.New(authdb.ConnectionOptions{
		Address: "auth-db:6379",
	})

	if err != nil {
		log.Fatal("Error creating DB client:", err)
	}

	if err := db.Connect(ctx); err != nil {
		log.Fatalln("Error connecting to DB:", err)
	}

	log.Println("DB connected")

	if err := db.Ping(ctx); err != nil {
		log.Fatalln("Error pinging DB:", err)
	}

	log.Println("DB pinged")

	return db
}

func initSignKey(ctx context.Context, db authdb.Db) error {
	buf := make([]byte, 1024)

	rand.Reader.Read(buf)

	randomSignKey := base64.StdEncoding.EncodeToString(buf)

	tokenSignKey, err := db.GetSharedValue(ctx, "auth.token.signKey", randomSignKey)

	if err != nil {
		return err
	}

	token.SignKey, err = base64.StdEncoding.DecodeString(tokenSignKey)

	return err
}
