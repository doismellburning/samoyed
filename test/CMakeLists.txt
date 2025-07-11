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
