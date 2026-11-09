package store

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"cakefactory/internal/model"
	"cakefactory/internal/timing"
)

// opTimeout bounds every database call: with MongoDB down a station must carry
// on after a moment instead of waiting for the driver's 30 s default.
const opTimeout = timing.DBTimeout

// Mongo stores pan documents and node state snapshots in MongoDB.
type Mongo struct {
	pans  *mongo.Collection
	state *mongo.Collection
}

// NewMongo connects to MongoDB and verifies the connection.
func NewMongo(ctx context.Context, uri string) (*Mongo, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := mongo.Connect(cctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(2*time.Second))
	if err != nil {
		return nil, err
	}
	if err := c.Ping(cctx, nil); err != nil {
		return nil, err
	}
	db := c.Database("cakefactory")
	return &Mongo{pans: db.Collection("pans"), state: db.Collection("node_state")}, nil
}

// Save upserts a pan document.
func (m *Mongo) Save(ctx context.Context, p model.Pan) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	_, err := m.pans.ReplaceOne(ctx, bson.M{"_id": p.ID}, p, options.Replace().SetUpsert(true))
	return err
}

// Get loads one pan document.
func (m *Mongo) Get(ctx context.Context, id string) (model.Pan, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	var p model.Pan
	err := m.pans.FindOne(ctx, bson.M{"_id": id}).Decode(&p)
	if err == mongo.ErrNoDocuments {
		return p, false, nil
	}
	return p, err == nil, err
}

// List returns the most recently updated pans.
func (m *Mongo) List(ctx context.Context, limit int) ([]model.Pan, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	cur, err := m.pans.Find(ctx, bson.M{}, options.Find().SetSort(bson.M{"updated": -1}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	var out []model.Pan
	return out, cur.All(ctx, &out)
}

// SaveState upserts a node snapshot.
func (m *Mongo) SaveState(ctx context.Context, node string, state map[string]int64) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	_, err := m.state.ReplaceOne(ctx, bson.M{"_id": node}, bson.M{"_id": node, "state": state, "at": time.Now()},
		options.Replace().SetUpsert(true))
	return err
}

// LoadState loads a node snapshot; a missing snapshot is not an error.
func (m *Mongo) LoadState(ctx context.Context, node string) (map[string]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	var doc struct {
		State map[string]int64 `bson:"state"`
	}
	err := m.state.FindOne(ctx, bson.M{"_id": node}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	return doc.State, err
}
