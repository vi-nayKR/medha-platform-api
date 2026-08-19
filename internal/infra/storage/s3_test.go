package storage

import "testing"

func TestS3EndpointHost(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{
			name:     "bare host",
			endpoint: "localhost:8333",
			want:     "localhost:8333",
		},
		{
			name:     "https url",
			endpoint: "https://s3-dev.medha.dev",
			want:     "s3-dev.medha.dev",
		},
		{
			name:     "http url with trailing slash",
			endpoint: "http://localhost:8333/",
			want:     "localhost:8333",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s3EndpointHost(tt.endpoint); got != tt.want {
				t.Fatalf("s3EndpointHost(%q) = %q, want %q", tt.endpoint, got, tt.want)
			}
		})
	}
}

func TestS3EndpointSecure(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		fallback bool
		want     bool
	}{
		{
			name:     "http is insecure",
			endpoint: "http://localhost:8333",
			fallback: true,
			want:     false,
		},
		{
			name:     "https is secure",
			endpoint: "https://s3-dev.medha.dev",
			fallback: false,
			want:     true,
		},
		{
			name:     "bare endpoint uses fallback",
			endpoint: "localhost:8333",
			fallback: false,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s3EndpointSecure(tt.endpoint, tt.fallback); got != tt.want {
				t.Fatalf("s3EndpointSecure(%q, %v) = %v, want %v", tt.endpoint, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestS3EndpointBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		secure   bool
		want     string
	}{
		{
			name:     "cloudflare hostname gets https",
			endpoint: "s3-dev.medha.dev",
			secure:   true,
			want:     "https://s3-dev.medha.dev",
		},
		{
			name:     "localhost can stay http",
			endpoint: "http://localhost:8333",
			secure:   true,
			want:     "http://localhost:8333",
		},
		{
			name:     "url path is removed",
			endpoint: "https://s3-dev.medha.dev/browser/medha-dev",
			secure:   true,
			want:     "https://s3-dev.medha.dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s3EndpointBaseURL(tt.endpoint, tt.secure); got != tt.want {
				t.Fatalf("s3EndpointBaseURL(%q, %v) = %q, want %q", tt.endpoint, tt.secure, got, tt.want)
			}
		})
	}
}

func TestResolveURLForBucket(t *testing.T) {
	client := &S3Client{
		publicBaseURL: "https://s3-dev.medha.dev",
	}

	got := client.ResolveURLForBucket(BucketProfiles, "user-id/photo.jpg")
	want := "https://s3-dev.medha.dev/profiles/user-id/photo.jpg"
	if got != want {
		t.Fatalf("ResolveURLForBucket() = %q, want %q", got, want)
	}
}
