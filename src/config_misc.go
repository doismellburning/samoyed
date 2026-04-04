package direwolf

// handleLOGDIR handles the LOGDIR keyword.
func handleLOGDIR(ps *parseState) bool {
	/*
	 * LOGDIR	- Directory name for automatically named daily log files.  Use "." for current working directory.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing directory name for LOGDIR on line %d.\n", ps.line)

		return true
	} else {
		if ps.misc.log_path != "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: LOGDIR on line %d is replacing an earlier LOGDIR or LOGFILE.\n", ps.line)
		}

		ps.misc.log_daily_names = true
		ps.misc.log_path = t
	}

	t = split("", false)
	if t != "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: LOGDIR on line %d should have directory path and nothing more.\n", ps.line)
	}
	return false
}

// handleLOGFILE handles the LOGFILE keyword.
func handleLOGFILE(ps *parseState) bool {
	/*
	 * LOGFILE	- Log file name, including any directory part.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing file name for LOGFILE on line %d.\n", ps.line)

		return true
	} else {
		if ps.misc.log_path != "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: LOGFILE on line %d is replacing an earlier LOGDIR or LOGFILE.\n", ps.line)
		}

		ps.misc.log_daily_names = false
		ps.misc.log_path = t
	}

	t = split("", false)
	if t != "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: LOGFILE on line %d should have file name and nothing more.\n", ps.line)
	}
	return false
}
