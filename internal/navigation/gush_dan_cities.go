package navigation

import (
	"log"
)

// InitializeGushDanCities sets up entry points for major cities in Gush Dan
func InitializeGushDanCities(router *EntryPointRouter) {
	g := router.Graph
	epm := router.EntryPointManager

	// Define major cities in Gush Dan with approximate centers and radii
	cities := []struct {
		name    string
		centerX float64 // longitude
		centerY float64 // latitude
		radius  float64 // km
		entries int     // number of entry points
	}{
		{"Tel Aviv", 34.7818, 32.0853, 5.0, 5},
		{"Ramat Gan", 34.8247, 32.0809, 3.0, 4},
		{"Petah Tikva", 34.8878, 32.0871, 4.0, 4},
		{"Bnei Brak", 34.8338, 32.0809, 2.0, 3},
		{"Holon", 34.7742, 32.0117, 3.0, 4},
		{"Bat Yam", 34.7500, 32.0167, 2.5, 3},
		{"Ramat HaSharon", 34.8394, 32.1465, 2.5, 3},
		{"Herzliya", 34.8422, 32.1624, 3.0, 4},
		{"Givatayim", 34.8119, 32.0697, 1.5, 3},
		{"Rishon LeZion", 34.7913, 31.9730, 4.0, 4},
	}

	virtualIDCounter := -1

	for _, city := range cities {
		log.Printf("Identifying entry points for %s...", city.name)
		epm.IdentifyEntryPoints(g, city.name, city.centerX, city.centerY, city.radius, city.entries)

		entryPoints, exists := epm.GetEntryPoints(city.name)
		if !exists || len(entryPoints) == 0 {
			log.Printf("  Warning: No entry points found for %s", city.name)
			continue
		}
		log.Printf("  Found %d entry points for %s: %v", len(entryPoints), city.name, entryPoints)

		cityObj := epm.Cities[city.name]

		// Create forward virtual node (edges FROM each entry TO this node)
		fwdID := virtualIDCounter
		virtualIDCounter--
		g.AddVirtualNode(fwdID, city.name+"_entries_forward", city.centerX, city.centerY)
		cityObj.ForwardVirtualNodeID = fwdID
		for _, epID := range entryPoints {
			edgeID := virtualIDCounter
			virtualIDCounter--
			g.AddVirtualEdge(edgeID, epID, fwdID)
		}

		// Create reversed virtual node (edges FROM this node TO each entry)
		revID := virtualIDCounter
		virtualIDCounter--
		g.AddVirtualNode(revID, city.name+"_entries_reversed", city.centerX, city.centerY)
		cityObj.ReversedVirtualNodeID = revID
		for _, epID := range entryPoints {
			edgeID := virtualIDCounter
			virtualIDCounter--
			g.AddVirtualEdge(edgeID, revID, epID)
		}

		log.Printf("  Created virtual nodes for %s: forward=%d, reversed=%d", city.name, fwdID, revID)
	}

	// Log statistics
	totalNodes := 0
	for _, cityNodes := range epm.NodeToCity {
		if cityNodes != "" {
			totalNodes++
		}
	}
	log.Printf("Entry point system initialized: %d cities, %d nodes mapped", len(epm.Cities), totalNodes)
}
