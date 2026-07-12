package direwolf

// CM108Thing describes one USB audio/HID device found by CM108Inventory.
type CM108Thing struct {
	VID           int
	PID           int
	Product       string
	DevnodeSound  string
	Plughw        string
	Plughw2       string
	Devpath       string
	DevnodeHidraw string
	DevnodeUSB    string
}

// CM108Inventory takes inventory of USB audio and HID devices, up to maxThings entries.
func CM108Inventory(maxThings int) ([]CM108Thing, error) {
	var things, err = cm108_inventory(maxThings)
	if err != nil {
		return nil, err
	}

	var result = make([]CM108Thing, len(things))
	for i, thing := range things {
		result[i] = CM108Thing{
			VID:           thing.vid,
			PID:           thing.pid,
			Product:       thing.product,
			DevnodeSound:  thing.devnode_sound,
			Plughw:        thing.plughw,
			Plughw2:       thing.plughw2,
			Devpath:       thing.devpath,
			DevnodeHidraw: thing.devnode_hidraw,
			DevnodeUSB:    thing.devnode_usb,
		}
	}

	return result, nil
}

// CM108SetGPIOPin sets one GPIO pin of the CM108 or similar device.
func CM108SetGPIOPin(name string, num int, state int) int {
	return cm108_set_gpio_pin(name, num, state)
}
