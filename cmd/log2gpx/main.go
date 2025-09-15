package main

import (
	"cmp"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

/*
 * Information we gather for each thing.
 */

type thing_t struct {
	lat     float64
	lon     float64
	alt     float64 /* Meters above average sea level. */
	course  float64
	speed   float64 /* Meters per second. */
	time    string
	name    string
	desc    string /* freq/offset/tone something like 146.955 MHz -600k PL 74.4 */
	comment string /* Combined mic-e status and comment text */
}

var things []thing_t

const UNKNOWN_VALUE = float64(-999) /* Special value to indicate unknown altitude, speed, course. */

func KNOTS_TO_METERS_PER_SEC(x float64) float64 {
	return ((x) * 0.51444444444)
}

func main() {
	/*
	 * Read files listed or stdin if none.
	 */
	if len(os.Args) == 1 {
		read_csv(os.Stdin)
	} else {
		for i, arg := range os.Args {
			if i == 0 {
				continue
			}

			if arg == "-" {
				read_csv(os.Stdin)
			} else {
				var fp, err = os.Open(arg) //nolint:gosec
				if err == nil {
					read_csv(fp)
				} else {
					fmt.Fprintf(os.Stderr, "Can't open %s for read: %s\n", arg, err)
					os.Exit(1)
				}
			}
		}
	}

	if len(things) == 0 {
		fmt.Fprintf(os.Stderr, "Nothing to process.\n")
		os.Exit(1)
	}

	/*
	 * Sort the data so everything for the same name is adjacent and
	 * in order of time.
	 */

	slices.SortFunc(things, func(a, b thing_t) int {
		if n := strings.Compare(a.name, b.name); n != 0 {
			return n
		}

		return cmp.Compare(a.time, b.time)
	})

	// for (i=0; i<num_things; i++) {
	//  printf ("%d: %s %.6f %.6f %.1f %s\n",
	//    i,
	//    things[i].time,
	//    things[i].lat,
	//    things[i].lon,
	//    things[i].alt,
	//    things[i].name);
	//}

	/*
	 * GPX file header.
	 */
	fmt.Printf("<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n")
	fmt.Printf("<gpx version=\"1.1\" creator=\"Dire Wolf\">\n")

	/*
	 * Group together all records for the same entity.
	 */
	var first = 0

	var last = 0
	for first < len(things) {
		for last < len(things)-1 && things[first].name == things[last+1].name {
			last++
		}

		process_things(first, last)
		first = last + 1
	}

	/*
	 *  GPX file tail.
	 */
	fmt.Printf("</gpx>\n")
}

/*
 * Read from given file, already open, into things array.
 */
func read_csv(fp *os.File) {
	var reader = csv.NewReader(fp)

	for {
		var fields, err = reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			panic(err)
		}

		var pchan = fields[0]

		var putime = fields[1]

		var pisotime = fields[2]

		var psource = fields[3]

		var pheard = fields[4]

		var plevel = fields[5]

		var perror = fields[6]

		var pdti = fields[7]

		var pname = fields[8]

		var psymbol = fields[9]

		var platitude = fields[10]

		var plongitude = fields[11]

		var pspeed = fields[12] /* Knots, must convert. */

		var pcourse = fields[13]

		var paltitude = fields[14] /* Meters, already correct units. */

		var pfreq = fields[15]

		var poffset = fields[16]

		var ptone = fields[17]

		var psystem = fields[18]

		var pstatus = fields[19]

		var ptelemetry = fields[20] /* Currently unused.  Add to description? */

		var pcomment = fields[21]

		/* Suppress the 'set but not used' warnings. */
		_ = ptelemetry
		_ = psystem
		_ = psymbol
		_ = pdti
		_ = perror
		_ = plevel
		_ = pheard
		_ = psource
		_ = putime

		/*
		 * Skip header line with names of fields.
		 */
		if pchan == "chan" {
			continue
		}

		/*
		 * Save only if we have valid data.
		 * (Some packets don't contain a position.)
		 */
		if len(pisotime) > 0 &&
			len(pname) > 0 &&
			len(platitude) > 0 &&
			len(plongitude) > 0 {
			var thing thing_t

			var speed = UNKNOWN_VALUE

			var course = UNKNOWN_VALUE

			var alt = UNKNOWN_VALUE

			var stemp string

			var desc string

			var comment string

			if len(pspeed) > 0 {
				var fspeed, _ = strconv.ParseFloat(pspeed, 64)

				speed = KNOTS_TO_METERS_PER_SEC(fspeed)
			}

			if len(pcourse) > 0 {
				course, _ = strconv.ParseFloat(pcourse, 64)
			}

			if len(paltitude) > 0 {
				alt, _ = strconv.ParseFloat(paltitude, 64)
			}

			/* combine freq/offset/tone into one description string. */

			if len(pfreq) > 0 {
				var freq, _ = strconv.ParseFloat(pfreq, 64)

				desc = fmt.Sprintf("%.3f MHz", freq)
			}

			if len(poffset) > 0 {
				var offset, _ = strconv.Atoi(poffset)
				if offset != 0 && offset%1000 == 0 {
					stemp = fmt.Sprintf("%+dM", offset/1000)
				} else {
					stemp = fmt.Sprintf("%+dk", offset)
				}

				if len(desc) > 0 {
					desc += " "
				}

				desc += stemp
			}

			if len(ptone) > 0 {
				if ptone[0] == 'D' {
					stemp = "DCS " + ptone[1:]
				} else {
					stemp = "PL " + ptone
				}

				if len(desc) > 0 {
					desc += " "
				}

				desc += stemp
			}

			if len(pstatus) > 0 {
				comment = pstatus
			}

			if len(pcomment) > 0 {
				if len(comment) > 0 {
					comment += ", "
				}

				comment += pcomment
			}

			thing.lat, _ = strconv.ParseFloat(platitude, 64)
			thing.lon, _ = strconv.ParseFloat(plongitude, 64)
			thing.speed = speed
			thing.course = course
			thing.alt = alt
			thing.time = pisotime
			thing.name = pname
			thing.desc = desc
			thing.comment = comment

			things = append(things, thing)
		}
	}
}

/*
 * Prepare text values for XML.
 * Replace significant characters with "predefined entities."
 */

func xml_text(in string) string {
	var out string

	for _, p := range in {
		switch p {
		case '"':
			out += "&quot;"
		case '\'':
			out += "&apos;"
		case '<':
			out += "&lt;"
		case '>':
			out += "&gt;"
		default:
			out += string(p)
		}
	}

	return out
}

/*
 * Process all things with the same name.
 * They should be sorted by time.
 * For stationary entities, generate just one GPX waypoint.
 * For moving entities, generate a GPX track.
 */

func process_things(first int, last int) {
	var moved bool

	for i := first + 1; i <= last; i++ {
		if things[i].lat != things[first].lat {
			moved = true
		}

		if things[i].lon != things[first].lon {
			moved = true
		}
	}

	if moved {
		/*
		 * Generate track for moving thing.
		 */
		var safe_name = xml_text(things[first].name)

		var safe_comment = xml_text(things[first].comment)

		fmt.Printf("  <trk>\n")
		fmt.Printf("    <name>%s</name>\n", safe_name)
		fmt.Printf("    <trkseg>\n")

		for i := first; i <= last; i++ {
			fmt.Printf("      <trkpt lat=\"%.6f\" lon=\"%.6f\">\n", things[i].lat, things[i].lon)

			if things[i].speed != UNKNOWN_VALUE {
				fmt.Printf("        <speed>%.1f</speed>\n", things[i].speed)
			}

			if things[i].course != UNKNOWN_VALUE {
				fmt.Printf("        <course>%.1f</course>\n", things[i].course)
			}

			if things[i].alt != UNKNOWN_VALUE {
				fmt.Printf("        <ele>%.1f</ele>\n", things[i].alt)
			}

			if len(things[i].desc) > 0 {
				fmt.Printf("        <desc>%s</desc>\n", things[i].desc)
			}

			if len(safe_comment) > 0 {
				fmt.Printf("        <cmt>%s</cmt>\n", safe_comment)
			}

			fmt.Printf("        <time>%s</time>\n", things[i].time)
			fmt.Printf("      </trkpt>\n")
		}

		fmt.Printf("    </trkseg>\n")
		fmt.Printf("  </trk>\n")
	}

	// Future possibility?
	// <sym>Symbol Name</sym>	-- not standardized.

	/*
	 * Generate waypoint for stationary thing or last known position for moving thing.
	 */
	var safe_name = xml_text(things[last].name)

	var safe_comment = xml_text(things[last].comment)

	fmt.Printf("  <wpt lat=\"%.6f\" lon=\"%.6f\">\n", things[last].lat, things[last].lon)

	if things[last].alt != UNKNOWN_VALUE {
		fmt.Printf("    <ele>%.1f</ele>\n", things[last].alt)
	}

	if len(things[last].desc) > 0 {
		fmt.Printf("    <desc>%s</desc>\n", things[last].desc)
	}

	if len(safe_comment) > 0 {
		fmt.Printf("    <cmt>%s</cmt>\n", safe_comment)
	}

	fmt.Printf("    <name>%s</name>\n", safe_name)
	fmt.Printf("  </wpt>\n")
}
