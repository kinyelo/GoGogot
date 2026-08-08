#!/usr/bin/env python3
import json, datetime

data = {
    "location": "New York, NY",
    "fetched_at": "2026-07-26T12:00 EDT",
    "temperature_c": 28.0,
    "temperature_f": round(28.0 * 9/5 + 32, 1),
    "humidity": 38,
    "wind_kmh": 5.2,
    "conditions": "Sunny"
}

report = f"""Weather Report
=============
Location: {data['location']}
Time:     {data['fetched_at']}
Temp:     {data['temperature_c']}°C / {data['temperature_f']}°F
Humidity: {data['humidity']}%
Wind:     {data['wind_kmh']} km/h
"""

with open("/data/weather_report.md", "w") as f:
    f.write(report)

with open("/data/weather_data.json", "w") as f:
    json.dump(data, f, indent=2)

print("Done. Files saved to /data/")
print(report)
