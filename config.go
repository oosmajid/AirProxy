package main

// buildConfig کانفیگ کامل Xray را با اینباند SOCKS (و اختیاری HTTP) و اوت‌باند داده‌شده می‌سازد.
// اگر routing غیرِnil باشد، بخش routing (قوانین bypass) هم اضافه می‌شود.
func buildConfig(listen string, socksPort, httpPort int, outbound map[string]interface{}, routing map[string]interface{}) map[string]interface{} {
	inbounds := []map[string]interface{}{
		{
			"tag":      "socks-in",
			"listen":   listen,
			"port":     socksPort,
			"protocol": "socks",
			"settings": map[string]interface{}{
				"udp":  true,
				"auth": "noauth",
			},
			"sniffing": map[string]interface{}{
				"enabled":      true,
				"destOverride": []string{"http", "tls"},
			},
		},
	}

	if httpPort > 0 {
		inbounds = append(inbounds, map[string]interface{}{
			"tag":      "http-in",
			"listen":   listen,
			"port":     httpPort,
			"protocol": "http",
			"settings": map[string]interface{}{},
		})
	}

	cfg := map[string]interface{}{
		"log": map[string]interface{}{
			"loglevel": "warning",
		},
		"inbounds": inbounds,
		"outbounds": []map[string]interface{}{
			outbound,
			{"tag": "direct", "protocol": "freedom"},
			{"tag": "block", "protocol": "blackhole"},
		},
	}
	if routing != nil {
		cfg["routing"] = routing
	}
	return cfg
}
