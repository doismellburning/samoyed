
# global includes
# not ideal but not so slow
# otherwise use target_include_directories
include_directories(
  ${GPSD_INCLUDE_DIRS}
  ${HAMLIB_INCLUDE_DIRS}
  ${ALSA_INCLUDE_DIRS}
  ${UDEV_INCLUDE_DIRS}
  ${PORTAUDIO_INCLUDE_DIRS}
  ${SNDIO_INCLUDE_DIRS}
  ${CUSTOM_GEOTRANZ_DIR}
  ${GPIOD_INCLUDE_DIRS}
  )

if(WIN32 OR CYGWIN)
  include_directories(
    ${CUSTOM_HIDAPI_DIR}
    ${CUSTOM_REGEX_DIR}
  )
endif()

if(APPLE)
  include_directories(
    ${HIDAPI_INCLUDE_DIRS}
  )
endif()


# direwolf
list(APPEND direwolf_SOURCES
  direwolf.c
  ais.c
  aprs_tt.c
  audio_stats.c
  ax25_link.c
  ax25_pad.c
  ax25_pad2.c
  beacon.c
  config.c
  decode_aprs.c
  deviceid.c
  dedupe.c
  demod_9600.c
  demod_afsk.c
  demod_psk.c
  demod.c
  digipeater.c
  cdigipeater.c
  dlq.c
  dsp.c
  dtime_now.c
  dtmf.c
  dwgps.c
  dwsock.c
  encode_aprs.c
  encode_aprs.c
  fcs_calc.c
  fcs_calc.c
  fx25_encode.c
  fx25_extract.c
  fx25_init.c
  fx25_rec.c
  fx25_send.c
  fx25_auto.c
  gen_tone.c
  hdlc_rec.c
  hdlc_rec2.c
  hdlc_send.c
  igate.c
  il2p_codec.c
  il2p_scramble.c
  il2p_rec.c
  il2p_payload.c
  il2p_init.c
  il2p_header.c
  il2p_send.c
  kiss_frame.c
  kiss.c
  kissserial.c
  kissnet.c
  latlong.c
  latlong.c
  log.c
  morse.c
  multi_modem.c
  waypoint.c
  nettnc.c
  serial_port.c
  pfilter.c
  ptt.c
  recv.c
  rrbb.c
  server.c
  symbols.c
  telemetry.c
  textcolor.c
  tq.c
  tt_text.c
  tt_user.c
  xid.c
  xmit.c
  dwgps.c
  dwgpsnmea.c
  dwgpsd.c
  mheard.c
  )

if(LINUX)
  list(APPEND direwolf_SOURCES
    audio.c
    )
  if(UDEV_FOUND)
    list(APPEND direwolf_SOURCES
      cm108.c
      )
  endif()
  if(AVAHI_CLIENT_FOUND)
    list(APPEND direwolf_SOURCES
      dns_sd_common.c
      dns_sd_avahi.c
      )
  endif()
  elseif(WIN32 OR CYGWIN) # windows
    list(APPEND direwolf_SOURCES
      audio_win.c
      cm108.c

      # icon
      # require plain gcc binary or link
      ${CMAKE_SOURCE_DIR}/cmake/cpack/direwolf.rc
      )
    list(REMOVE_ITEM direwolf_SOURCES
      dwgpsd.c
      )
  elseif(HAVE_SNDIO)
    list(APPEND direwolf_SOURCES
      audio.c
      )
  else() # macOS freebsd
    list(APPEND direwolf_SOURCES
      audio_portaudio.c
      cm108.c
      )
    if(USE_MACOS_DNSSD)
      list(APPEND direwolf_SOURCES
        dns_sd_common.c
        dns_sd_macos.c
        )
    endif()
endif()

add_executable(direwolf
  ${direwolf_SOURCES}
  )

target_link_libraries(direwolf
  ${GEOTRANZ_LIBRARIES}
  ${MISC_LIBRARIES}
  ${REGEX_LIBRARIES}
  ${HIDAPI_LIBRARIES}
  Threads::Threads
  ${GPSD_LIBRARIES}
  ${HAMLIB_LIBRARIES}
  ${ALSA_LIBRARIES}
  ${UDEV_LIBRARIES}
  ${PORTAUDIO_LIBRARIES}
  ${GPIOD_LIBRARIES}
  ${SNDIO_LIBRARIES}
  ${AVAHI_LIBRARIES}
  )

set(DIREWOLF_COMPILE_FLAGS "-DC_BUILD")
if(WIN32 OR CYGWIN)
  set(DIREWOLF_COMPILE_FLAGS "${DIREWOLF_COMPILE_FLAGS} -DUSE_REGEX_STATIC")
  target_link_libraries(direwolf winmm ws2_32 setupapi)
endif()

set_target_properties(direwolf
  PROPERTIES COMPILE_FLAGS "${DIREWOLF_COMPILE_FLAGS}"
  )

# Test application to generate sound.
# gen_packets
list(APPEND gen_packets_SOURCES
  gen_packets.c
  ax25_pad.c
  ax25_pad2.c
  fx25_encode.c
  fx25_extract.c
  fx25_init.c
  fx25_send.c
  hdlc_send.c
  fcs_calc.c
  gen_tone.c
  il2p_codec.c
  il2p_scramble.c
  il2p_payload.c
  il2p_init.c
  il2p_header.c
  il2p_send.c
  morse.c
  dtmf.c
  textcolor.c
  dsp.c
  )

add_executable(gen_packets
  ${gen_packets_SOURCES}
  )

target_link_libraries(gen_packets
  ${MISC_LIBRARIES}
  )


install(TARGETS direwolf DESTINATION ${INSTALL_BIN_DIR})
install(TARGETS gen_packets DESTINATION ${INSTALL_BIN_DIR})
