#include <stddef.h>
#include "ax25_pad.h"
#include "aprs_tt.h"

struct tt_config_s *aprs_tt_config;

struct ttloc_s *ttloc_ptr_get(int idx) {
	return &aprs_tt_config->ttloc_ptr[idx];
}

double ttloc_ptr_get_point_lat(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].point.lat;
}

double ttloc_ptr_get_point_lon(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].point.lon;
}

double ttloc_ptr_get_vector_lat(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].vector.lat;
}

double ttloc_ptr_get_vector_lon(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].vector.lon;
}

double ttloc_ptr_get_vector_scale(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].vector.scale;
}

double ttloc_ptr_get_grid_lat0(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].grid.lat0;
}

double ttloc_ptr_get_grid_lat9(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].grid.lat9;
}

double ttloc_ptr_get_grid_lon0(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].grid.lon0;
}

double ttloc_ptr_get_grid_lon9(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].grid.lon9;
}

double ttloc_ptr_get_utm_scale(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].utm.scale;
}

double ttloc_ptr_get_utm_x_offset(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].utm.x_offset;
}

double ttloc_ptr_get_utm_y_offset(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].utm.y_offset;
}

long ttloc_ptr_get_utm_lzone(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].utm.lzone;
}

char ttloc_ptr_get_utm_latband(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].utm.latband;
}

char ttloc_ptr_get_utm_hemi(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].utm.hemi;
}

char *ttloc_ptr_get_mgrs_zone(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].mgrs.zone;
}

char *ttloc_ptr_get_mhead_prefix(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].mhead.prefix;
}

char *ttloc_ptr_get_macro_definition(int idx) {
	return aprs_tt_config->ttloc_ptr[idx].macro.definition;
}

struct ttloc_s aprs_tt_test_config[] = {
	{ TTLOC_POINT, "B01", .point.lat = 12.25, .point.lon = 56.25 },
	{ TTLOC_POINT, "B988", .point.lat = 12.50, .point.lon = 56.50 },

	{ TTLOC_VECTOR, "B5bbbdddd", .vector.lat = 53., .vector.lon = -1., .vector.scale = 1000. },  // km units

	// Hilltop Tower http://www.aprs.org/aprs-jamboree-2013.html
	{ TTLOC_VECTOR, "B5bbbddd", .vector.lat = 37+55.37/60., .vector.lon = -(81+7.86/60.), .vector.scale = 16.09344 },   // .01 mile units

	{ TTLOC_GRID, "B2xxyy", .grid.lat0 = 12.00, .grid.lon0 = 56.00,
				.grid.lat9 = 12.99, .grid.lon9 = 56.99 },
	{ TTLOC_GRID, "Byyyxxx", .grid.lat0 = 37 + 50./60.0, .grid.lon0 = 81,
				.grid.lat9 = 37 + 59.99/60.0, .grid.lon9 = 81 + 9.99/60.0 },

	{ TTLOC_MHEAD, "BAxxxxxx", .mhead.prefix = "326129" },

	{ TTLOC_SATSQ, "BAxxxx" },

	{ TTLOC_MACRO, "xxyyy", .macro.definition = "B9xx*AB166*AA2B4C5B3B0Ayyy" },
	{ TTLOC_MACRO, "xxxxzzzzzzzzzz", .macro.definition = "BAxxxx*ACzzzzzzzzzz" },
};
