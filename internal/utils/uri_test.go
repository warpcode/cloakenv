package utils

import "testing"

func TestParseURI(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		scheme   string
		location string
		wantErr  bool
	}{
		{
			name:     "valid uri",
			uri:      "keyring://service/account",
			scheme:   "keyring",
			location: "service/account",
			wantErr:  false,
		},
		{
			name:     "valid uri with complex location",
			uri:      "search://tags=prod&title=db/Password",
			scheme:   "search",
			location: "tags=prod&title=db/Password",
			wantErr:  false,
		},
		{
			name:    "missing scheme",
			uri:     "://location",
			wantErr: true,
		},
		{
			name:     "empty location allowed for single entity",
			uri:      "scheme://",
			scheme:   "scheme",
			location: "",
			wantErr:  false,
		},
		{
			name:    "missing separator",
			uri:     "keyring-location",
			wantErr: true,
		},
		{
			name:    "empty string",
			uri:     "",
			wantErr: true,
		},
		{
			name:    "null byte in scheme",
			uri:     "key\x00ring://service/account",
			wantErr: true,
		},
		{
			name:    "null byte in location",
			uri:     "keyring://service\x00/account",
			wantErr: true,
		},
		{
			name:    "null byte in parameter",
			uri:     "search://tags=prod\x00&title=db/Password",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, location, err := ParseURI(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseURI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if scheme != tt.scheme {
					t.Errorf("ParseURI() scheme = %v, want %v", scheme, tt.scheme)
				}
				if location != tt.location {
					t.Errorf("ParseURI() location = %v, want %v", location, tt.location)
				}
			}
		})
	}
}
