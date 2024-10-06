package utils

import (
	"context"
	"log"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

var Client *mongo.Client
var DB *mongo.Database

func ConnectDB(mongoURI string, ctx context.Context) {
	var err error
	Client, err = mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatal("连接错误：", err)
	}

	if err = Client.Ping(context.TODO(), readpref.Primary()); err != nil {
		log.Fatal(err)
	}
	log.Println("Connected to MongoDB")
	DB = Client.Database("auth-server")
}

func GetDB() *mongo.Database {
	return DB
}
