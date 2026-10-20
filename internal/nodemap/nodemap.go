// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package nodemap reports a node to the UK Packet Radio network map at
// https://nodes.ukpacketradio.network/, which plots the nodes that report to
// it.
//
// A report is a JSON description of the node POSTed to
// /api/nodedata/{callsign}, whose schema, NodeData, is in the map's OpenAPI
// description at /swagger/v1/swagger.json.
package nodemap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/latlong"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/version"
	"github.com/sirupsen/logrus"
)

// DefaultURL is where the map is.
const DefaultURL = "https://nodes.ukpacketradio.network"

// defaultMapPage is where on the map a reported node can be seen.
const defaultMapPage = DefaultURL + "/maps/packet"

// interval is how often the node reports.
const interval = time.Hour

// softwareName is what the map is told the node runs.
const softwareName = "Samoyed"

// Config is what the map is told about the node.
type Config struct {
	// Address is the node's address - its callsign, with any SSID - which
	// the map knows it by.
	Address string

	// Alias is the node's alias, or empty for none.
	Alias string

	// APIURL is the base URL of the map's API, or empty for DefaultURL's.
	APIURL string

	// Location is where the node is, if the map is to be told.
	Location maybe.Maybe[Location]

	// Contact is how to reach whoever runs the node, or empty for nothing.
	Contact string

	// Comment is a short note about the node, or empty for none.
	Comment string

	// Ports are the interfaces the node links over.
	Ports []Port
}

// Report is the map's NodeData: what a node says about itself.
type Report struct {
	NodeAlias    string                `json:"nodeAlias,omitempty"`
	Location     maybe.Maybe[Location] `json:"location,omitzero"`
	Software     Software              `json:"software"`
	Contact      string                `json:"contact,omitempty"`
	SysopComment string                `json:"sysopComment,omitempty"`
	Ports        []Port                `json:"ports,omitzero"`
}

// Location is where a node is: a Maidenhead locator, the middle of the
// square it names, and how high its antenna is, if the map is to be told.
type Location struct {
	Locator string `json:"locator"`
	Coords  LatLon `json:"coords"`

	// ASL is the antenna's height above sea level, in metres.
	ASL maybe.Maybe[int32] `json:"asl,omitzero"`
}

// LatLon is a position in degrees, north and east positive.
type LatLon struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// LocationAt is the location locator, a Maidenhead locator such as IO91wm,
// names, or what is wrong with it.
func LocationAt(locator string) (Location, error) {
	var lat, lon, err = latlong.FromGridSquare(locator)
	if err != nil {
		var none Location

		return none, err
	}

	return Location{Locator: locator, Coords: LatLon{Lat: lat, Lon: lon}, ASL: maybe.Nothing[int32]()}, nil
}

// Port is an interface a node links over: for Samoyed, a channel.  Anything
// not known is left out, for the map to treat as unknown.
type Port struct {
	// ID is what the map knows the port by.
	ID string `json:"id"`

	LinkType LinkType `json:"linkType"`

	// Freq is the port's frequency, in Hz.
	Freq maybe.Maybe[int64] `json:"freq,omitzero"`

	// Mode is the layer 2 protocol, such as AX.25 or IL2P.
	Mode string `json:"mode,omitempty"`

	// Modulation is such as AFSK, FSK or QPSK.
	Modulation string `json:"modulation,omitempty"`

	// Baud is symbols per second.
	Baud maybe.Maybe[int32] `json:"baud,omitzero"`

	// Bitrate is bits per second.
	Bitrate maybe.Maybe[int32] `json:"bitrate,omitzero"`

	// Usage is what the port is for, such as Access or Backbone.
	Usage string `json:"usage,omitempty"`

	Comment string `json:"comment,omitempty"`
}

// LinkType is what sort of link a port is.
type LinkType string

const (
	LinkRF         LinkType = "RF"
	LinkPrivateNet LinkType = "PrivateNet"
	LinkInternet   LinkType = "Internet"
	LinkOther      LinkType = "Other"
)

// ParseLinkType is the link type name names, if it is one the map knows.
func ParseLinkType(name string) (LinkType, bool) {
	var linkType = LinkType(name)

	switch linkType {
	case LinkRF, LinkPrivateNet, LinkInternet, LinkOther:
		return linkType, true
	default:
		return "", false
	}
}

// Software is what the node runs.
type Software struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// Reporter reports a node to the map.
type Reporter struct {
	config Config
	client *http.Client

	// url is where the map is.
	url string
}

// New makes a reporter that tells the map about the node config describes,
// using client.
func New(config Config, client *http.Client) *Reporter {
	var r = new(Reporter)

	r.config = config
	r.client = client
	r.url = strings.TrimSuffix(config.APIURL, "/")
	if r.url == "" {
		r.url = DefaultURL
	}

	return r
}

// Report is what the node says about itself.
func (r *Reporter) Report() Report {
	return Report{
		NodeAlias:    r.config.Alias,
		Location:     r.config.Location,
		Software:     Software{Name: softwareName, Version: version.Version},
		Contact:      r.config.Contact,
		SysopComment: r.config.Comment,
		Ports:        r.config.Ports,
	}
}

// Run reports the node to the map now and every hour after, until ctx is
// cancelled.  A report that fails is logged, and the next is sent as usual.
func (r *Reporter) Run(ctx context.Context) {
	logrus.WithFields(logrus.Fields{
		"node": r.config.Address,
		"map":  r.where(),
	}).Info("Reporting the node to the node map")

	for ctx.Err() == nil {
		var err = r.send(ctx)

		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			logrus.WithError(err).WithField("node", r.config.Address).Warn("Unable to report the node to the node map")
		default:
			logrus.WithField("node", r.config.Address).Debug("Reported the node to the node map")
		}

		if !dwutil.SleepCtx(ctx, interval) {
			return
		}
	}
}

// where is where to see the node: the public map's page of nodes, or, for
// another map, the API it is reported to, since that map's pages could be
// anywhere.
func (r *Reporter) where() string {
	if r.url == DefaultURL {
		return defaultMapPage
	}

	return r.url
}

// send sends the map one report.
func (r *Reporter) send(ctx context.Context) error {
	body, marshalErr := json.Marshal(r.Report()) // := for errchkjson, which doesn't follow var.
	if marshalErr != nil {
		return fmt.Errorf("encoding report: %w", marshalErr)
	}

	var endpoint = r.url + "/api/nodedata/" + url.PathEscape(r.config.Address)

	var req, reqErr = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if reqErr != nil {
		return fmt.Errorf("making request: %w", reqErr)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent())

	var resp, respErr = r.client.Do(req)
	if respErr != nil {
		return fmt.Errorf("sending report: %w", respErr)
	}

	defer resp.Body.Close()

	// Read what is left, so the connection can be used again.
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("node map answered %s", resp.Status)
	}

	return nil
}

// userAgent is how a report says what sent it: Samoyed, and its version if
// it has one.
func userAgent() string {
	if version.Version == "" {
		return softwareName
	}

	return softwareName + "/" + version.Version
}
