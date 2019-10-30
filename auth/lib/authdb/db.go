package authdb

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pkg/errors"
	"github.com/bsm/redislock"
	"github.com/go-redis/redis"
	"github.com/opentracing/opentracing-go"

	"github.com/Evertras/events-demo/auth/lib/tracing"
)

// UserEntry is a single row in the database
type UserEntry struct {
	// ID is some uniquely generated identifier attached to the user
	ID string

	// Email is the user's email address
	Email string

	// PasswordHash is the hashed/salted password that will be used
	// for comparison/validation
	PasswordHashWithSalt string
}

// Db is a persistent database that stores user information
type Db interface {
	// Connect actually connects to the database
	Connect(ctx context.Context) error

	// Ping pings the database to check connectivity
	Ping(ctx context.Context) error

	// CreateUser creates a user in the database
	CreateUser(ctx context.Context, entry UserEntry) error

	// GetUserByEmail returns the user's entry or nil if not found.
	// Returns an error if something unexpected goes wrong.
	GetUserByEmail(ctx context.Context, email string) (*UserEntry, error)

	// GetSharedValue returns a stored value; if it does not exist,
	// it will atomically store the given value and return that.
	GetSharedValue(ctx context.Context, key string, ifNotExist string) (string, error)

	// WaitForCreateUser will wait for the user to be created
	// in the database before returning, or an error if the user
	// was not seen as added before the context cancels
	WaitForCreateUser(ctx context.Context, email string) error
}

type db struct {
	db   *redis.Client
	opts ConnectionOptions
	tracer opentracing.Tracer
}

type ConnectionOptions struct {
	Address string
}

func New(opts ConnectionOptions) (Db, error) {
	tracer, err := tracing.Init("redis")

	if err != nil {
		return nil, errors.Wrap(err, "failed to init tracer")
	}

	return &db{
		db:   nil,
		opts: opts,
		tracer: tracer,
	}, nil
}

func (d *db) Connect(ctx context.Context) error {
	d.db = redis.NewClient(&redis.Options{
		Addr:     d.opts.Address,
		Password: "",
		DB:       0,
	})

	return d.Ping(ctx)
}

func (d *db) Ping(ctx context.Context) error {
	span, ctx := opentracing.StartSpanFromContextWithTracer(ctx, d.tracer, "Ping")
	defer span.Finish()

	if d.db == nil {
		return errors.New("db not connected")
	}

	return d.db.Ping().Err()
}

func (d *db) CreateUser(ctx context.Context, entry UserEntry) error {
	span, ctx := opentracing.StartSpanFromContextWithTracer(ctx, d.tracer, "CreateUser")
	defer span.Finish()

	if entry.Email == "" {
		return errors.New("must supply email")
	}

	val, err := json.Marshal(entry)

	if err != nil {
		return errors.Wrap(err, "failed to marshal to JSON")
	}

	err = d.db.Set(credsKey(entry.Email), val, 0).Err()

	return err
}

func (d *db) WaitForCreateUser(ctx context.Context, email string) error {
	span, ctx := opentracing.StartSpanFromContextWithTracer(ctx, d.tracer, "WaitForCreateUser")
	defer span.Finish()

	ps := d.db.PSubscribe("__keyspace@*__:creds:" + email)

	defer ps.Close()

	_, err := ps.Receive()

	if err != nil {
		return err
	}

	select {
	case <-ps.Channel():
		return nil

	case <-ctx.Done():
		return errors.New("context finished before message received")
	}
}

func (d *db) GetUserByEmail(ctx context.Context, email string) (*UserEntry, error) {
	span, ctx := opentracing.StartSpanFromContextWithTracer(ctx, d.tracer, "GetUserByEmail")
	defer span.Finish()

	entry := &UserEntry{}

	raw, err := d.db.Get(credsKey(email)).Result()

	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}

		return nil, errors.Wrap(err, "failed to get key")
	}

	err = json.Unmarshal([]byte(raw), entry)

	if err != nil {
		return nil, errors.Wrap(err, "found key but could not unmarshal json")
	}

	return entry, nil
}

func credsKey(email string) string {
	return "creds:" + email
}

func (d *db) GetSharedValue(ctx context.Context, key string, ifNotExist string) (string, error) {
	span, ctx := opentracing.StartSpanFromContextWithTracer(ctx, d.tracer, "GetSharedValue")
	defer span.Finish()

	locker := redislock.New(d.db)

	lockKey := key + ".lock"

	lock, err := locker.Obtain(
		lockKey,
		50*time.Millisecond,
		nil,
	)

	if err != nil {
		return "", err
	}

	defer lock.Release()

	d.db.SetNX(key, ifNotExist, 0)

	actualID, err := d.db.Get(key).Result()

	return actualID, err
}
