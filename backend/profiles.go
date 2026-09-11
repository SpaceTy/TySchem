package main

// UserStats aggregates the feedback received by a user's uploads.
type UserStats struct {
	Uploads   int     `json:"uploads"`
	Likes     int     `json:"likes"`
	AvgRating float64 `json:"avgRating"`
	Ratings   int     `json:"ratings"`
}

// UserProfile is the public view of an account plus its highlights.
type UserProfile struct {
	User   User                `json:"user"`
	Stats  UserStats           `json:"stats"`
	Best   []SchematicMetadata `json:"best"`
	Latest []SchematicMetadata `json:"latest"`
}

const profileHighlightLimit = 6

// AggregateFeedback totals likes and ratings across everything userID owns.
func (s *Store) AggregateFeedback(userID string) (UserStats, error) {
	var st UserStats
	err := s.db.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM schematics WHERE owner_id = ?),
			(SELECT COUNT(*) FROM likes l JOIN schematics s ON s.id = l.schematic_id WHERE s.owner_id = ?),
			(SELECT COALESCE(AVG(r.rating), 0) FROM ratings r JOIN schematics s ON s.id = r.schematic_id WHERE s.owner_id = ?),
			(SELECT COUNT(*) FROM ratings r JOIN schematics s ON s.id = r.schematic_id WHERE s.owner_id = ?)`,
		userID, userID, userID, userID,
	).Scan(&st.Uploads, &st.Likes, &st.AvgRating, &st.Ratings)
	return st, err
}

// Profile returns the public account view for username along with the owner's
// top-rated and most recent schematics (with viewer-specific feedback filled).
func (s *Store) Profile(username, viewerID string) (UserProfile, error) {
	u, err := s.GetUserByUsername(username)
	if err != nil {
		return UserProfile{}, err
	}
	stats, err := s.AggregateFeedback(u.ID)
	if err != nil {
		return UserProfile{}, err
	}
	best, _, err := s.List(ListFilter{
		OwnerID: u.ID, Sort: "rating", Order: "desc",
		Limit: profileHighlightLimit, ViewerID: viewerID,
	})
	if err != nil {
		return UserProfile{}, err
	}
	latest, _, err := s.List(ListFilter{
		OwnerID: u.ID, Sort: "uploadDate", Order: "desc",
		Limit: profileHighlightLimit, ViewerID: viewerID,
	})
	if err != nil {
		return UserProfile{}, err
	}
	if best == nil {
		best = []SchematicMetadata{}
	}
	if latest == nil {
		latest = []SchematicMetadata{}
	}
	return UserProfile{User: u, Stats: stats, Best: best, Latest: latest}, nil
}
