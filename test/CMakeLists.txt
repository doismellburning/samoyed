# This is a trick to avoid more complication
# because configure_file() is done at configuration time

set(GEN_PACKETS_BIN "${CMAKE_BINARY_DIR}/src/gen_packets${CMAKE_EXECUTABLE_SUFFIX}")
set(ATEST_BIN "${CMAKE_BINARY_DIR}/src/atest${CMAKE_EXECUTABLE_SUFFIX}")

if(WIN32)
  set(CUSTOM_SCRIPT_SUFFIX ".bat")
else()
  set(CUSTOM_SCRIPT_SUFFIX "")
endif()

# generate the scripts that run the tests

# global includes
# not ideal but not so slow
# otherwise use target_include_directories
include_directories(
  ${CUSTOM_SRC_DIR}
  ${GPSD_INCLUDE_DIRS}
  ${HAMLIB_INCLUDE_DIRS}
  ${ALSA_INCLUDE_DIRS}
  ${UDEV_INCLUDE_DIRS}
  ${PORTAUDIO_INCLUDE_DIRS}
  ${CUSTOM_GEOTRANZ_DIR}
  ${CMAKE_BINARY_DIR}/src
  )

if(WIN32 OR CYGWIN)
  include_directories(
    ${CUSTOM_REGEX_DIR}
  )
endif()

if(WIN32 OR CYGWIN)
  list(REMOVE_ITEM atest9_SOURCES
    ${CUSTOM_SRC_DIR}/dwgpsd.c
    )
endif()


# doing ctest on previous programs

#  -----------------------------  Manual tests and experiments  ---------------------------
if(OPTIONAL_TEST)

  # Unit test for IGate
  list(APPEND itest_SOURCES
    ${CUSTOM_SRC_DIR}/igate.c
    ${CUSTOM_SRC_DIR}/ais.c
    ${CUSTOM_SRC_DIR}/ax25_pad.c
    ${CUSTOM_SRC_DIR}/fcs_calc.c
    ${CUSTOM_SRC_DIR}/mheard.c
    ${CUSTOM_SRC_DIR}/pfilter.c
    ${CUSTOM_SRC_DIR}/telemetry.c
    ${CUSTOM_SRC_DIR}/decode_aprs.c
    ${CUSTOM_SRC_DIR}/deviceid.c
    ${CUSTOM_SRC_DIR}/dwgpsnmea.c
    ${CUSTOM_SRC_DIR}/dwgps.c
    ${CUSTOM_SRC_DIR}/dwgpsd.c
    ${CUSTOM_SRC_DIR}/serial_port.c
    ${CUSTOM_SRC_DIR}/textcolor.c
    ${CUSTOM_SRC_DIR}/dtime_now.c
    ${CUSTOM_SRC_DIR}/latlong.c
    ${CUSTOM_SRC_DIR}/tt_text.c
    ${CUSTOM_SRC_DIR}/symbols.c
    )

  if(WIN32 OR CYGWIN)
    list(REMOVE_ITEM itest_SOURCES
      ${CUSTOM_SRC_DIR}/dwgpsd.c
      )
  endif()

  add_executable(itest
    ${itest_SOURCES}
    )

  set_target_properties(itest
    PROPERTIES COMPILE_FLAGS "-DITEST"
    )

  target_link_libraries(itest
    ${MISC_LIBRARIES}
    ${GPSD_LIBRARIES}
    Threads::Threads
    )

  if(WIN32 OR CYGWIN)
    target_link_libraries(itest ws2_32)
  endif()


  # For demodulator tweaking experiments.
  list(APPEND testagc_SOURCES
    ${CUSTOM_SRC_DIR}/atest.c
    ${CUSTOM_SRC_DIR}/ais.c
    ${CUSTOM_SRC_DIR}/demod.c
    ${CUSTOM_SRC_DIR}/dsp.c
    ${CUSTOM_SRC_DIR}/demod_afsk.c
    ${CUSTOM_SRC_DIR}/demod_psk.c
    ${CUSTOM_SRC_DIR}/demod_9600.c
    ${CUSTOM_SRC_DIR}/hdlc_rec.c
    ${CUSTOM_SRC_DIR}/hdlc_rec2.c
    ${CUSTOM_SRC_DIR}/multi_modem.c
    ${CUSTOM_SRC_DIR}/rrbb.c
    ${CUSTOM_SRC_DIR}/fcs_calc.c
    ${CUSTOM_SRC_DIR}/ax25_pad.c
    ${CUSTOM_SRC_DIR}/decode_aprs.c
    ${CUSTOM_SRC_DIR}/deviceid.c
    ${CUSTOM_SRC_DIR}/dwgpsnmea.c
    ${CUSTOM_SRC_DIR}/dwgps.c
    ${CUSTOM_SRC_DIR}/dwgpsd.c
    ${CUSTOM_SRC_DIR}/serial_port.c
    ${CUSTOM_SRC_DIR}/telemetry.c
    ${CUSTOM_SRC_DIR}/dtime_now.c
    ${CUSTOM_SRC_DIR}/latlong.c
    ${CUSTOM_SRC_DIR}/tt_text.c
    ${CUSTOM_SRC_DIR}/symbols.c
    ${CUSTOM_SRC_DIR}/textcolor.c
    )

  if(WIN32 OR CYGWIN)
    list(REMOVE_ITEM testagc_SOURCES
      ${CUSTOM_SRC_DIR}/dwgpsd.c
      )
  endif()

  add_executable(testagc
    ${testagc_SOURCES}
    )

  target_link_libraries(testagc
    ${MISC_LIBRARIES}
    ${GPSD_LIBRARIES}
    Threads::Threads
    )

  if(WIN32 OR CYGWIN)
    target_link_libraries(testagc ws2_32)
  endif()


  # Send GPS location to KISS TNC each second.
  list(APPEND walk96_SOURCES
    ${CUSTOM_SRC_DIR}/walk96.c
    ${CUSTOM_SRC_DIR}/ais.c
    ${CUSTOM_SRC_DIR}/dwgps.c
    ${CUSTOM_SRC_DIR}/dwgpsnmea.c
    ${CUSTOM_SRC_DIR}/dwgpsd.c
    ${CUSTOM_SRC_DIR}/kiss_frame.c
    ${CUSTOM_SRC_DIR}/latlong.c
    ${CUSTOM_SRC_DIR}/encode_aprs.c
    ${CUSTOM_SRC_DIR}/serial_port.c
    ${CUSTOM_SRC_DIR}/textcolor.c
    ${CUSTOM_SRC_DIR}/ax25_pad.c
    ${CUSTOM_SRC_DIR}/fcs_calc.c
    ${CUSTOM_SRC_DIR}/xmit.c
    ${CUSTOM_SRC_DIR}/xid.c
    ${CUSTOM_SRC_DIR}/hdlc_send.c
    ${CUSTOM_SRC_DIR}/gen_tone.c
    ${CUSTOM_SRC_DIR}/ptt.c
    ${CUSTOM_SRC_DIR}/tq.c
    ${CUSTOM_SRC_DIR}/hdlc_rec.c
    ${CUSTOM_SRC_DIR}/hdlc_rec2.c
    ${CUSTOM_SRC_DIR}/rrbb.c
    ${CUSTOM_SRC_DIR}/dsp.c
    ${CUSTOM_SRC_DIR}/multi_modem.c
    ${CUSTOM_SRC_DIR}/demod.c
    ${CUSTOM_SRC_DIR}/demod_afsk.c
    ${CUSTOM_SRC_DIR}/demod_psk.c
    ${CUSTOM_SRC_DIR}/demod_9600.c
    ${CUSTOM_SRC_DIR}/server.c
    ${CUSTOM_SRC_DIR}/morse.c
    ${CUSTOM_SRC_DIR}/dtmf.c
    ${CUSTOM_SRC_DIR}/audio_stats.c
    ${CUSTOM_SRC_DIR}/dtime_now.c
    ${CUSTOM_SRC_DIR}/dlq.c
    )

  if(LINUX)
    list(APPEND walk96_SOURCES
      ${CUSTOM_SRC_DIR}/audio.c
      )
    if(UDEV_FOUND)
      list(APPEND walk96_SOURCES
        ${CUSTOM_SRC_DIR}/cm108.c
        )
    endif()
  elseif(WIN32 OR CYGWIN) # windows
    list(APPEND walk96_SOURCES
      ${CUSTOM_SRC_DIR}/audio_win.c
      )
    list(REMOVE_ITEM walk96_SOURCES
      ${CUSTOM_SRC_DIR}/dwgpsd.c
      )
  else() # macOS freebsd openbsd
    list(APPEND walk96_SOURCES
      ${CUSTOM_SRC_DIR}/audio_portaudio.c
      )
  endif()

  add_executable(walk96
    ${walk96_SOURCES}
    )

  set_target_properties(walk96
    PROPERTIES COMPILE_FLAGS "-DWALK96 -DUSE_REGEX_STATIC"
    )

  target_link_libraries(walk96
    ${MISC_LIBRARIES}
    ${REGEX_LIBRARIES}
    ${GPSD_LIBRARIES}
    ${HAMLIB_LIBRARIES}
    ${ALSA_LIBRARIES}
    ${PORTAUDIO_LIBRARIES}
    ${UDEV_LIBRARIES}
    Threads::Threads
    )

  if(WIN32 OR CYGWIN)
    target_link_libraries(walk96 ws2_32)
  endif()


  # TODO  miss the audio file

  # testagc
  # ./atest -P H+ -F 0 ../01_Track_1.wav ../02_Track_2.wav | grep "packets decoded in" >atest.out

  # testagc3
  # ./gen_packets -B 300 -n 100 -o noisy3.wav
  # ./atest3 -B 300 -P D -D 3 noisy3.wav | grep "packets decoded in" >atest.out

  # testagc96
  # ./gen_packets -B 9600 -n 100 -o noisy96.wav
  # ./atest96 -B 9600 ../walkabout9600c.wav noisy96.wav zzz16.wav zzz16.wav zzz16.wav zzz8.wav zzz8.wav zzz8.wav | grep "packets decoded in" >atest.out

  # testagc24
  # ./atest24 -B 2400 test2400.wav | grep "packets decoded in" >atest.out

  # testagc24mfj
  # ./atest24mfj -F 1 -B 2400 ../ref-doc/MFJ-2400-PSK/2k4_short.wav

  # testagc48
  # ./atest48 -B 4800 test4800.wav | grep "packets decoded in" >atest.out
endif()  # OPTIONAL_TEST
