package main

import (
	"net"
	"strings"

	"github.com/knadh/listmonk/models"
	"github.com/oschwald/geoip2-golang"
)

func initGeoIP(path string) *geoip2.Reader {
	if strings.TrimSpace(path) == "" {
		lo.Print("GeoIP City database not configured; campaign opens will have no location")
		return nil
	}

	reader, err := geoip2.Open(path)
	if err != nil {
		lo.Fatalf("error opening GeoIP City database: %v", err)
	}
	if !strings.Contains(strings.ToLower(reader.Metadata().DatabaseType), "city") {
		reader.Close()
		lo.Fatal("privacy.geoip_database must point to a GeoIP2-compatible City database")
	}
	lo.Printf("GeoIP City database loaded: %s", reader.Metadata().DatabaseType)
	return reader
}

// lookupCampaignOpenLocation never retains the request IP. The City database
// supplies approximate coordinates, which should not be treated as GPS data.
func lookupCampaignOpenLocation(reader *geoip2.Reader, ipString string) models.CampaignOpenLocation {
	var location models.CampaignOpenLocation
	if reader == nil {
		return location
	}
	ip := net.ParseIP(ipString)
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return location
	}

	city, err := reader.City(ip)
	if err != nil || city == nil {
		return location
	}
	location.CountryCode = city.Country.IsoCode
	location.Country = city.Country.Names["en"]
	if location.CountryCode == "" {
		location.CountryCode = city.RegisteredCountry.IsoCode
		location.Country = city.RegisteredCountry.Names["en"]
	}
	if location.CountryCode == "" {
		return models.CampaignOpenLocation{}
	}
	if len(city.Subdivisions) > 0 {
		location.Region = city.Subdivisions[0].Names["en"]
		if location.Region == "" {
			location.Region = city.Subdivisions[0].IsoCode
		}
	}
	location.City = city.City.Names["en"]
	lat, lon := city.Location.Latitude, city.Location.Longitude
	if lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 && (lat != 0 || lon != 0) {
		location.Latitude, location.Longitude = &lat, &lon
	}
	return location
}
