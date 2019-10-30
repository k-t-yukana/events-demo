package server

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/Evertras/events-demo/auth/lib/auth"
	"github.com/Evertras/events-demo/auth/lib/token"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
)

type RegisterBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func registerHandler(tracer opentracing.Tracer, a auth.Auth) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		spanCtx, _ := tracer.Extract(opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(r.Header))
		span := tracer.StartSpan("register", ext.RPCServerOption(spanCtx))
		defer span.Finish()

		ctx = opentracing.ContextWithSpan(ctx, span)

		if r.Method != "POST" {
			w.WriteHeader(400)
			log.Println("Method must be POST")
			return
		}

		body, err := ioutil.ReadAll(r.Body)

		if err != nil {
			w.WriteHeader(500)
			log.Println("Failed to parse:", err)
			return
		}

		var login LoginBody
		err = json.Unmarshal(body, &login)

		if err != nil {
			w.WriteHeader(400)
			log.Println("Could not parse body:", err)
			return
		}

		if login.Email == "" {
			w.WriteHeader(400)
			log.Println("Missing email address")
			return
		}

		if login.Password == "" {
			w.WriteHeader(400)
			log.Println("Missing password")
			return
		}

		_, err = a.Register(ctx, login.Email, login.Password)

		if err != nil {
			if err == auth.ErrUserAlreadyExists {
				w.WriteHeader(400)
				log.Println("User already exists:", login.Email)
				return
			}
			w.WriteHeader(500)
			log.Println("Failed to register user:", err)
			return
		}

		valid, err := a.Validate(ctx, login.Email, login.Password)

		if err != nil {
			w.WriteHeader(500)
			log.Println("Failed to validate:", err)
			return
		}

		if !valid {
			w.WriteHeader(400)
			log.Println("Failed to validate credentials for", login.Email)
			return
		}

		t, err := token.New(login.Email)

		if err != nil {
			w.WriteHeader(500)
			log.Println("Failed to generate token:", err)
			return
		}

		resBody, err := json.Marshal(TokenResponse{Token: t})

		if err != nil {
			w.WriteHeader(500)
			log.Println("Failed to marshal token response:", err)
			return
		}

		_, err = w.Write(resBody)

		if err != nil {
			w.WriteHeader(500)
			log.Println("Failed to write body:", err)
			return
		}
	}
}
