# generate conf per platform
file(READ "${CUSTOM_CONF_DIR}/generic.conf" file_content)

if(LINUX)
  string(REGEX REPLACE "\n%W%[^\n]*" "" file_content "${file_content}")
  string(REGEX REPLACE "\n%M%[^\n]*" "" file_content "${file_content}")
  string(REGEX REPLACE "\n%L%([^\n]*)" "\n\\1" file_content "${file_content}")
elseif(WIN32 OR CYGWIN)
  string(REGEX REPLACE "\n%M%[^\n]*" "" file_content "${file_content}")
  string(REGEX REPLACE "\n%L%[^\n]*" "" file_content "${file_content}")
  string(REGEX REPLACE "\n%W%([^\n]*)" "\n\\1" file_content "${file_content}")
else() # macOS FreeBSD OpenBSD
  string(REGEX REPLACE "\n%W%[^\n]*" "" file_content "${file_content}")
  string(REGEX REPLACE "\n%L%[^\n]*" "" file_content "${file_content}")
  string(REGEX REPLACE "\n%M%([^\n]*)" "\n\\1" file_content "${file_content}")
endif()

# remove remark
string(REGEX REPLACE "\n%R%[^\n]*" "" file_content "${file_content}")

# clear common lines
string(REGEX REPLACE "\n%C%([^\n]*)" "\n\\1" file_content "${file_content}")
string(REGEX REPLACE "^%C%([^\n]*)" "\\1" file_content "${file_content}")

file(WRITE "${CMAKE_BINARY_DIR}/direwolf.conf" "${file_content}")

# install udev rules for CM108
# There are two locations.  The one in /etc/udev/rules.d is meant for local customization and
# takes precedence for the same name.
# https://sources.debian.org/src/direwolf/1.7+dfsg-2/debian/patches/lib-udev-rules/
# says that we should use the /usr/lib/udev/rules.d location when building a package.
# TODO:  I think the proper solution is to select the location based on whether
# the application installation location is /usr/local or /usr.
if(LINUX)
  if (CMAKE_INSTALL_PREFIX STREQUAL "/usr/local") 
    install(FILES "${CUSTOM_CONF_DIR}/99-direwolf-cmedia.rules" DESTINATION /etc/udev/rules.d/)
  else()
    install(FILES "${CUSTOM_CONF_DIR}/99-direwolf-cmedia.rules" DESTINATION /usr/lib/udev/rules.d/)
  endif()
endif()

install(FILES "${CMAKE_BINARY_DIR}/direwolf.conf" DESTINATION ${INSTALL_CONF_DIR})
install(FILES "${CUSTOM_CONF_DIR}/sdr.conf" DESTINATION ${INSTALL_CONF_DIR})

# Put sample configuration & startup files in home directory.
# This step would be done as ordinary user.
# Some people like to put the direwolf config file in /etc/ax25.
# Note that all of these are also in $(DESTDIR)/share/doc/direwolf/examples/.
if(NOT (WIN32 OR CYGWIN))
  add_custom_target(install-conf
    COMMAND ${CMAKE_COMMAND}
    -DCUSTOM_BINARY_DIR="${CMAKE_BINARY_DIR}"
    -DCUSTOM_CONF_DIR="${CUSTOM_CONF_DIR}"
    -DCUSTOM_SCRIPTS_DIR="${CUSTOM_SCRIPTS_DIR}"
    -DCUSTOM_TELEMETRY_DIR="${CUSTOM_TELEMETRY_DIR}"
    -P "${CMAKE_SOURCE_DIR}/conf/install_conf.cmake"
    )
endif()
