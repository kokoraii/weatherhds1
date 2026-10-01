// WeatherHDS system and presentation configuration.

export let brand = {
    "networkName": "METEOchannel",
    "networkLogo": "/images/meteoch.png",
    "providers": { // where we are carried
        "Mist Weather Media": { // main sponsor
            "providerLogo": "/images/providers/mistlogo2026site.webp",
            "showOnLDLDur": 120, // how long to show the provider logo on the ldl in seconds. set to 0 to always show, and set to null to never show.
            "channels": {
              "sspwxr01": { "videoType": "hdtv", "label": "METEOchannel in HD" }, // shows a specific channel ID and label depending on the current videoType.
              "sspwxr02": { "videoType": "vga", "label": "METEOchannel in SD" },
            },
            "extraText": { // throw this text either before or after the provider logo on the LDL.
              "heading": null,
              "tailing": "weather media",
              "font": "Arial, sans-serif",
              "style": "italic", // normal, italic, or oblique
              "weight": "600", // normal is 400, bold is 700, you can also use numeric values from 100 to 900.
              "size": "1em",
              "shadow": "4px 5px 1px rgba(0, 0, 0, 0.45)" // text-shadow for the extra text, in CSS format. if null, no shadow will be applied.
            }
        },
    }
}

export let clock = {
    "24Hour": false, // 12 or 24 hour format for the clock.
    "showSeconds": true, // whether or not to show seconds on the clock.
    "rotateInterval": 0, // how often to cycle between timezones specified below, in seconds. set to 0 to disable cycling and just show the first timezone in the list.
    "hideDuplicateZones": true, // if a timezone in the list has the same current time as the previous one, hide it to avoid redundancy. i.e, winnipeg and regina have the same timezone offset during standard time, so ignore winnipeg and move on to the next zone.
    "zones": [
      "system",
      "America/Vancouver",
      "America/Edmonton",
      "America/Winnipeg",
      "America/Toronto",
      "America/Halifax",
      "America/St_Johns"
    ]
}

export let config = {
    "videoBackgrounds": true, // enables or disables the video backgrounds on the current conditions slide.
    "currentConditionsGradient": true, // current conditions gradient based on current time relative to sunrise or sunset.
    "staticIcons": false, // would you like icons that dont move?

    "performance": {
      "disableLiveBackdropBlur": true, // turns expensive live CSS backdrop blur into static glass surfaces.
      "cachedBackdropGlass": true, // pre-renders a blurred/tinted wallpaper copy and reuses it for glass panels.
      "glassCacheScale": 0.42, // lower = faster and softer, higher = sharper but more memory.
      "glassCacheBlurPx": 28,
      "lightweightMotion": true, // avoids JS-driven per-frame decoration where a CSS transition or static state works.
      "cityTickerFps": 60, // ticker frame cap. 60 keeps the LDL ticker fluid on standard displays.
      "forecastTextSlideMs": 220,
      "mainRadarFrames": 48,
      "mainRadarLoops": 12,
      "sidebarRadarFrames": 24,
      "sidebarRadarLoops": Infinity,
      "radarFrameDelay": 150,
      "chartDevicePixelRatio": 1
    },

    "music": {
      "enabled": true,
      "websocketPath": "/music/ws",
      "artPath": "/music/art",
      "showWhenStopped": false,
      "ducking": {
        "enabled": true,
        "volume": 0.25,
        "fadeMs": 350,
        "releaseMs": 700
      }
    },

    "vocallocal": {
      "enabled": true, // whether or not to use vocallocal to narrate conditions and forecasts.
      "voice": {
        "engine": "auto", // auto tries native ONNX/sherpa first when the server is built with native_tts, then falls back to Piper.
        "name": "en_us-lessac-medium",
        "rate": -1,
        "pitch": 2,
        "volume": 100
      },
      "blackout": { // disable vocallocals during this timerange. useful for when you have an IntelliStar 2 and you dont want the AI to disrupt Jim Cantore's beautiful voiceovers.
        "enabled": true,
        "timing": [
          {
            "minuteInterval": null, // start the blackout every x minutes. so if this is set to 8, it will start a blackout at 12:08, 12:16, 12:24, and so on.
            "tenMinInterval": 8, // start the blackout on this minute inside every ten-minute block. so if this is set to 8, it will start at 12:08, 12:18, 12:28, and so on.
            "hourInterval": null, // or you can set it to start every x hours, so if this is set to 2, it will start a blackout at 12:00, 2:00, 4:00, and so on.
            "quarterDayInterval": null, // or you can set it to start every x quarter days (6 hours), so if this is set to 1, it will start a blackout at 12:00, 6:00, 12:00, 6:00, and so on.
            "dayInterval": null, // or you can set it to start every x days, so if this is set to 1, it will start a blackout at 12:00 every day, and so on.
            "duration": 2,
          }
        ]
      }
    },
    
    "presentationConfig": {
        "main": true,
        "ldl": true,
        "ldlClock": false,
        "ldlBack": true,
        "backgrounds": true,
        "repeatMain": true,
        "bannerLogo": true,
        "providerLogo": true,
        "autorunOnStartup": true // mainly for when i make new slides lol
      },
      
      "cityTicker": {
        "enabled": true, // enables or disables the city ticker above the LDL.
        "showTimeChanges": true, // whether or not to show time changes in the ticker. if true, it will show the time change icons and text for each city that has a different current time than the main location. if false, it will just show the city names without any indication of time changes.
        "products": [ // which products to show in the city ticker. options are:
          "currentConditions",
          "dayOne",
          "dayTwo"
        ],
        "severeAlertIcon": true, // shows a severe weather alert icon in the city ticker if there is an active severe weather alert in that location.
        "intervalMessages": [
          {
            "messageHeader": "Thanks for watching",
            "messageBody": "METEOchannel is Saskatoon's own continuous automated weather channel. Local weather, when you want it, when you DON'T want it, and everything in between.",
            "showBeforeProduct": "currentConditions", // which product to show the message before. options are the same as the products array above.
            "showAfterProduct": null, // which product to show the message after. options are the same as the products array above. if null, it will just show the message before the specified product and then continue with the normal ticker content.
            "scroll": true, // whether or not to scroll the message across the ticker. if false, it will just show the message in place of the normal ticker content for 15 seconds.
            "displayInterval": 2 // show this message every x cycles of the city ticker. so if you have 3 products in the ticker and this message is set to show before dayOne, and the displayInterval is set to 2, then it will show this message before dayOne every 2 cycles of the ticker. so it would show before dayOne on the first cycle, then not show on the second cycle, then show again on the third cycle, and so on.
          },
          {
            "messageHeader": "",
            "headerIcon": null, // you can also have messages with icons next to either or both the header and the body. just add the filename of the icon in the appropriate field, and make sure the icon is in the public folder.
            "messageBody": "",
            "bodyIcon": "/images/giphy.gif",
            "showBeforeProduct": "",
            "showAfterProduct": "",
            "scroll": true,
            "displayInterval": 30
          }
        ]
      },
      
      "topbarStyle": "domestic", // domestic or weatherscan.
      "videoType": "auto", // sets the aspect ratio
      "systemTimeZone": "America/Regina", // tz_database. https://en.wikipedia.org/wiki/List_of_tz_database_time_zones
      "tickerContent": "", // empty = hides scroll. add any text to it to show a crawl on the LDL.
    "overrideBackgroundImage": "", // skips the background rotation and uses the URL provided.
    "backgroundSource": "local", // set to local to use the locally stored default backgrounds, set value to online to use the Bing background api thing, and set to url to use a background URL.
    "verboseLogging": true, // enables or disables console.log
    "refreshInterval": 120 // how often to refresh the browser tab, in minutes.
  }
  
export const locationConfig = {
  mainBlockPlaylist: [ // this determines the order of playlists to show on the main block (large block covering most of the screen). it will cycle through each playlist in order, showing the location(s) specified by the index for 12 seconds before moving on to the next one. if a playlist has fewer locations than the specified index, it will loop back to the beginning of that playlist.
    { playlist: "primary", index: 0 }, // plays the first primary location found in localLocations
    { playlist: "secondary", index: 0 }, // plays the first set of secondary locations
    //  { playlist: "primary", index: 1 }, // plays the second primary location found in localLocations, if it exists. if not, ignores and moves on to the next item in the playlist.
    //  { playlist: "secondary", index: 1 }, // plays the second set of secondary locations, if it exists. if not, ignores and moves on to the next item in the playlist.
    { playlist: "bumper", bumperId: "nationalBumper", index: 0 }, // put our bumper graphic to open our national segment.
    { playlist: "national", regionId: "Pacific", index: 0 }, // plays the national playlist for the pacific location set as defined in nationalLocations.
    { playlist: "national", regionId: "Prairies", index: 0 },
    { playlist: "national", regionId: "Central", index: 0 },
    { playlist: "national", regionId: "Atlantic", index: 0 },
    { playlist: "bumper", bumperId: "USARegionalBumper", index: 0 }, // put our bumper graphic to open our US national segment.
    { playlist: "national", regionId: "Northeast", index: 0 }, // plays the national playlist for the northeast location set as defined in usaLocations.
    { playlist: "national", regionId: "Midwest", index: 0 },
    { playlist: "national", regionId: "South", index: 0 },
    { playlist: "national", regionId: "West", index: 0 },
    { playlist: "bumper", bumperId: "stationID", index: 0 }, // station ID graphic indicates a new cycle of the program.
  ],

  localLocations: [
    { 
      playlist: "primary", // a primary location of index 0 would be our main service location.
      index: 0,
      locations: [
        { name: "Saskatoon, SK, Canada", displayName: "Saskatoon, SK" }
      ]
    },
    {
      playlist: "secondary", // a secondary location of index 0 would be the first set of secondary locations. these are meant to be used for other important locations in your area, such as nearby cities, or cities where you have clients.
      index: 0,
      locations: [ // saskatoon area
        { name: "Prince Albert, SK, Canada", displayName: "Prince Albert" },
        { name: "North Battleford, SK, Canada", displayName: "North Battleford" },
        { name: "Outlook, SK, Canada", displayName: "Outlook" },
        { name: "Rosetown, SK, Canada", displayName: "Rosetown" },
        { name: "Melfort, SK, Canada", displayName: "Melfort" },
        { name: "Lloydminster, AB, Canada", displayName: "Lloydminster" },
      ]
    }


  ],

  ldlLocations: [ // unlike main, the LDL is a very simple endeavor.
    "Saskatoon, SK",
    "Outlook, SK",
    "Rosetown, SK",
    "Melfort, SK",
    "North Battleford, SK",
    "Lloydminster, AB",
    "Regina, SK"
  ],

  cityTickerLocations: {
    "categories": {
      "CENTRAL SASKATCHEWAN": [
        "Saskatoon, SK",
        "Prince Albert, SK",
        "Lloydminster, AB",
        "North Battleford, SK",
        "Humboldt, SK",
        "Melfort, SK",
        "Wadena, SK",
        "Watrous, SK",
        "Wynyard, SK",
        "Porcupine Plain, SK",
        "Tisdale, SK",
        "Kerrobert, SK",
        "Rosetown, SK",
        "Outlook, SK",
      ],
      "SOUTHERN SASKATCHEWAN": [
        "Regina, SK",
        "Moose Jaw, SK",
        "Weyburn, SK",
        "Estevan, SK",
        "Swift Current, SK",
        "Moosomin, SK",
        "Assiniboia, SK",
        "Maple Creek, SK",
        "Yorkton, SK",
        "Melville, SK",
        "Fort Qu'Appelle, SK",
        "Gravelbourg, SK",
      ]
    },
  },

  nationalLocations: { // 12 cities each. sort by province, then by population. these are used for the national weather slides showed on main.
    "regions": {
      "Pacific": {
      "mapCenter": [50.5000, -124.0000], 
      "zoomLevel": 5,
        "timezone": "America/Vancouver",
        "locations": [
          "Vancouver, BC",
          "Victoria, BC",
          "Kelowna, BC",
          "Prince George, BC",
          "Nanaimo, BC",
          "Abbotsford, BC",
          "Kamloops, BC",
          "Surrey, BC",
          "Burnaby, BC",
          "Richmond, BC",
          "Langley, BC",
          "Delta, BC"
        ]
      },
      "Prairies": {
        "mapCenter": [55.0000, -106.0000],
        "zoomLevel": 4,
        "timezone": "America/Regina",
        "locations": [
          "Calgary, AB",
          "Edmonton, AB",
          "Red Deer, AB",
          "Lethbridge, AB",
          "Medicine Hat, AB",
          "Saskatoon, SK",
          "Regina, SK",
          "Moose Jaw, SK",
          "Swift Current, SK",
          "Winnipeg, MB",
          "Brandon, MB",
          "Thompson, MB"
        ]
      },
      "Central": {
        "mapCenter": [46.5000, -78.5000],
        "zoomLevel": 4,
        "timezone": "America/Toronto",
        "locations": [
          "Toronto, ON",
          "Ottawa, ON",
          "Mississauga, ON",
          "Brampton, ON",
          "Thunder Bay, ON",
          "Timmins, ON",
          "Sudbury, ON",
          "Montreal, QC",
          "Quebec City, QC",
          "Laval, QC",
          "Gatineau, QC",
          "Longueuil, QC"
        ]
      },
      "Atlantic": {
        "mapCenter": [46.8000, -60.5000],
        "zoomLevel": 5,
        "timezone": "America/Halifax",
        "locations": [
          "Halifax, NS",
          "St. John's, NL",
          "Moncton, NB",
          "Fredericton, NB",
          "Saint John, NB",
          "Charlottetown, PE",
          "Sydney, NS",
          "Corner Brook, NL",
          "Bridgetown, NS",
          "Mount Pearl, NL",
          "Dartmouth, NS",
          "Truro, NS"
        ]
      }
    }
  },

  "usaLocations": { // same thing but for the US. 12 cities per region, sorted by population. also sorted by census bureau regions (northeast, midwest, south, and west).
    "regions": {
      "Northeast": {
        "mapCenter": [41.8000, -74.5000],
        "zoomLevel": 6,
        "timezone": "America/New_York",
        "locations": [
          "New York, NY",
          "Boston, MA",
          "Philadelphia, PA",
          "Pittsburgh, PA",
          "Buffalo, NY",
          "Rochester, NY",
          "Albany, NY",
          "Providence, RI",
          "Hartford, CT",
          "Worcester, MA",
          "Springfield, MA",
          "Bridgeport, CT"
        ]
      },
      "Midwest": {
        "mapCenter": [41.5000, -90.5000],
        "zoomLevel": 5,
        "timezone": "America/Chicago",
        "locations": [
          "Chicago, IL",
          "Indianapolis, IN",
          "Columbus, OH",
          "Detroit, MI",
          "Milwaukee, WI",
          "Kansas City, MO",
          "Omaha, NE",
          "Minneapolis, MN",
          "Cleveland, OH",
          "St. Louis, MO",
          "Cincinnati, OH",
          "Madison, WI"
        ]
      },
      "South": {
        "mapCenter": [33.5000, -89.0000],
        "zoomLevel": 4,
        "timezone": "America/Chicago",
        "locations": [
          "Houston, TX",
          "San Antonio, TX",
          "Dallas, TX",
          "Austin, TX",
          "Jacksonville, FL",
          "Fort Worth, TX",
          "Charlotte, NC",
          "El Paso, TX",
          "Nashville, TN",
          "Memphis, TN",
          "Oklahoma City, OK",
          "Louisville, KY"
        ]
      },
      "West": {
        "mapCenter": [39.5000, -114.0000],
        "zoomLevel": 4,
        "timezone": "America/Los_Angeles",
        "locations": [
          "Los Angeles, CA",
          "San Diego, CA",
          "San Jose, CA",
          "San Francisco, CA",
          "Phoenix, AZ",
          "Seattle, WA",
          "Denver, CO",
          "Portland, OR",
          "Las Vegas, NV",
          "Tucson, AZ",
          "Albuquerque, NM",
          "Fresno, CA"
        ]
      }
    }
  }
};

export const serverConfig = {
  "units": "m", // m for metric, e for imperial, h for hybrid, and s for metric SI
  "webPort": 3000,
  "cacheValidTime": 720, //in seconds
  "alertPollIntervalNormal": 30, // in minutes
  "alertPollIntervalSevere": 12, // in minutes, how often to poll for severe weather alerts when there is an active severe alert in the area.
}

export const displayUnits = {
  "e": {
    endingTemp: "°F",
    endingWind: "mph",
    endingDistance: "mi",
    endingMeasurement: "in",
    endingCeiling: "ft",
    endingPressure: "hg",
    endingSnow: "in",
    endingRain: "in"
  },
  "m": {
    endingTemp: "°C",
    endingWind: "km/h",
    endingDistance: "km",
    endingMeasurement: "mm",
    endingCeiling: "m",
    endingPressure: "mb",
    endingSnow: "cm",
    endingRain: "mm"
  },
  "s": {
    endingTemp: "°C",
    endingWind: "m/s",
    endingDistance: "m",
    endingMeasurement: "mm",
    endingCeiling: "ft",
    endingPressure: "mb",
    endingSnow: "cm",
    endingRain: "mm"
  },
  "h": {
    endingTemp: "°C",
    endingWind: "mph",
    endingDistance: "mi",
    endingMeasurement: "mm",
    endingCeiling: "ft",
    endingPressure: "mb",
    endingSnow: "cm",
    endingRain: "mm"
  }
}

export const bumperBackgroundsRandom = {
  "national": [
    {
      name: "Del Rosa Intersection",
      subtitle: "Manila, Philippines",
      author: "raii/SSPWXR",
      url: "/images/national_bg_images/3.webp"
    },
    {
      name: "SaskTel Corporate Office",
      subtitle: "Saskatoon, SK",
      author: "raii/SSPWXR",
      url: "/images/national_bg_images/4.webp"
    },
    {
      name: "Old City Hall",
      subtitle: "Toronto, ON",
      author: "Ali Cuhadaroglu, Pexels",
      url: "/images/national_bg_images/probably_toronto.webp"
    },
    {
      name: "Some Valley in Alberta",
      subtitle: "Banff, AB",
      author: "Ryutaro Tsukata, Pexels",
      url: "/images/national_bg_images/albernta.webp"
    },
    {
      name: "Random grain elevator in Saskatchewan",
      subtitle: "Saskatchewan, Canada",
      author: "Bryan Smith, Pexels",
      url: "/images/national_bg_images/oldfarmerthingymajig.webp"
    },
    {
      name: "Sunset over South Saskatchewan River",
      subtitle: "Outlook, SK",
      author: "raii/SSPWXR",
      url: "/images/national_bg_images/outlook_sunset_bridge.webp"
    },
    {
      name: "Downtown Edmonton",
      subtitle: "Edmonton, AB",
      author: "raii/SSPWXR",
      url: "/images/national_bg_images/downtn_edmonton.webp"
    },
    {
      name: "Fisherman's Wharf",
      subtitle: "Victoria, BC",
      author: "raii/SSPWXR",
      url: "/images/national_bg_images/vic_harbour1.webp"
    },
    {
      name: "Victoria Harbour",
      subtitle: "Victoria, BC",
      author: "raii/SSPWXR",
      url: "/images/national_bg_images/vic_harbour2.webp"
    }
  ],
  "stationId": [],
  "local": [],
  "special": []
}

export const weatherIcons = {
  "0": ["tornado.svg", "tornado.svg", "tornado.avif"],
  "1": ["hurricane.svg", "hurricane.svg", null],
  "2": ["hurricane.svg", "hurricane.svg", null],
  "3": ["thunderstorms-day-extreme-rain.svg", "thunderstorms-night-extreme-rain.svg", "storm.avif"],
  "4": ["thunderstorms-day.svg", "thunderstorms-night.svg", "strongTstm1.avif"],
  "5": ["overcast-day-sleet.svg", "overcast-night-sleet.svg", "sleetDay1.avif"],
  "6": ["overcast-day-sleet.svg", "overcast-night-sleet.svg", "sleetDay1.avif"],
  "7": ["overcast-day-sleet.svg", "overcast-night-sleet.svg", "sleetDay1.avif"],
  "8": ["overcast-day-sleet.svg", "overcast-night-sleet.svg", "sleetDay1.avif"],
  "9": ["drizzle.svg", "drizzle.svg", "rain1.avif"],
  "10": ["overcast-day-sleet.svg", "overcast-night-sleet.svg", "sleetDay1.avif"],
  "11": ["overcast-day-rain.svg", "overcast-night-rain.svg", "rainDay1.avif"],
  "12": ["overcast-day-rain.svg", "overcast-night-rain.svg", "rain.avif"],
  "13": ["overcast-day-snow.svg", "overcast-night-snow.svg", "snowDay1.avif"],
  "14": ["overcast-day-snow.svg", "overcast-night-snow.svg", "snowDay2.avif"],
  "15": ["extreme-day-snow.svg", "extreme-night-snow.svg", "snowstorm.avif"],
  "16": ["overcast-day-snow.svg", "overcast-night-snow.svg", "snow.avif"],
  "17": ["extreme-day-hail.svg", "extreme-night-hail.svg", null],
  "18": ["overcast-day-sleet.svg", "overcast-night-sleet.svg", "sleetDay1.avif"],
  "19": ["dust-wind.svg", "dust-wind.svg", null],
  "20": ["overcast-day-fog.svg", "overcast-night-fog.svg", "fog.avif"],
  "21": ["overcast-day-mist.svg", "overcast-night-mist.svg", "foggyDay1.avif"],
  "22": ["overcast-day-smoke.svg", "overcast-night-smoke.svg", null],
  "23": ["wind.svg", "wind.svg", "windyDay1.avif"],
  "24": ["wind.svg", "wind.svg", "windyDay2.avif"],
  "25": ["thermometer-colder.svg", "thermometer-colder.svg", "coldDay1.avif"],
  "26": ["cloudy.svg", "cloudy.svg", "cloudy.avif"],
  "27": ["overcast-night.svg", "overcast-night.svg", "mostlyCloudyNight1.avif"],
  "28": ["overcast-day.svg", "overcast-day.svg", "mostlyCloudyDay1.avif"],
  "29": ["partly-cloudy-night.svg", "partly-cloudy-night.svg", "partlyCloudyNight1.avif"],
  "30": ["partly-cloudy-day.svg", "partly-cloudy-day.svg", "partlyCloudyDay1.avif"],
  "31": ["clear-night.svg", "clear-night.svg", "clearNight1.avif"],
  "32": ["clear-day.svg", "clear-day.svg", "sunny.avif"],
  "33": ["clear-night.svg", "clear-night.svg", "clearNight1.avif"],
  "34": ["clear-day.svg", "clear-day.svg", "clearWinterDay1.avif"],
  "35": ["extreme-hail.svg", "extreme-night-hail.svg", null],
  "36": ["thermometer-sun.svg", "thermometer-sun.svg", "strongTstmNight1.avif"],
  "37": ["thunderstorms-day.svg", "thunderstorms-day.svg", "strongTstm1.avif"],
  "38": ["thunderstorms-day.svg", "thunderstorms-night.svg", "strongTstmNight1.avif"],
  "39": ["overcast-day-rain.svg", "overcast-day-rain.svg", "rainDay1.avif"],
  "40": ["extreme-day-rain.svg", "extreme-night-rain.svg", "rainDay1.avif"],
  "41": ["snowflake.svg", "snowflake.svg", "snowDay1.avif"],
  "42": ["extreme-day-snow.svg", "extreme-night-snow.svg", "snowstorm.avif"],
  "43": ["extreme-day-snow.svg", "extreme-night-snow.svg", "snowstorm.avif"],
  "44": ["not-available.svg", "not-available.svg", null],
  "45": ["partly-cloudy-night-rain.svg", "partly-cloudy-night-rain.svg", "rainNight1.avif"],
  "46": ["partly-cloudy-night-sleet.svg", "partly-cloudy-night-sleet.svg", "sleetDay1.avif"],
  "47": ["thunderstorms-night.svg", "thunderstorms-night.svg", "strongTstmNight1.avif"]
};


export const holidayMapping = {
    "01-01": { name: "New Year's Day", color0: "#ffffff", color1: "#000000", icon: "", banner: "" },
    "01-18": { name: "MLK Day", color0: "#d9a10c", color1: "#d9a10c", icon: "", banner: "" },
    "02-02": { name: "Groundhog Day", color0: "#611579", color1: "#611579", icon: "", banner: "" },
    "02-14": { name: "Valentine's Day", color0: "#dd1b89", color1: "#dd1b89", icon: "", banner: "" },
    "03-13": { name: "Spring Clock Change", color0: "#6bbc79", color1: "#6bbc79", icon: "", banner: "" },
    "03-17": { name: "St. Patrick's Day", color0: "#05860b", color1: "#05860b", icon: "", banner: "" },
    "03-20": { name: "First Day of Spring", color0: "#c881d1", color1: "#c881d1", icon: "", banner: "" },
    "03-27": { name: "Easter", color0: "#e875c3", color1: "#e875c3", icon: "", banner: "" },
    "04-01": { name: "April Fools' Day", color0: "#ffff66", color1: "#ffffff", icon: "", banner: "" },
    "04-22": { name: "Earth Day", color0: "#006400", color1: "#006400", icon: "", banner: "" },
    "04-29": { name: "Arbor Day", color0: "#406732", color1: "#406732", icon: "", banner: "" },
    "05-08": { name: "Mother's Day", color0: "#f88fa3", color1: "#f88fa3", icon: "", banner: "" },
    "05-30": { name: "Memorial Day", color0: "#174ed5", color1: "#174ed5", icon: "", banner: "" },
    "06-19": { name: "Father's Day", color0: "#5c820b", color1: "#5c820b", icon: "", banner: "" },
    "06-20": { name: "First Day of Summer", color0: "#e87106", color1: "#e87106", icon: "", banner: "" },
    "07-01": { name: "Canada Day", color0: "#ff0000", color1: "#ffffff", icon: "", banner: "" },
    "07-04": { name: "Independence Day (US)", color0: "#de2a16", color1: "#de2a16", icon: "", banner: "" },
    "09-05": { name: "Labor Day", color0: "#1c3258", color1: "#a52d30", icon: "", banner: "" },
    "09-22": { name: "First Day of Fall", color0: "#a74101", color1: "#a74101", icon: "", banner: "" },
    "10-13": { name: "Thanksgiving (Canada)", color0: "#b85423", color1: "#b85423", icon: "", banner: "" },
    "10-31": { name: "Halloween", color0: "#d79f00", color1: "#d79f00", icon: "", banner: "" },
    "11-06": { name: "Fall Clock Change", color0: "#b3ad00", color1: "#b3ad00", icon: "", banner: "" },
    "11-11": { name: "Remembrance Day (CA) / Veterans Day (US)", color0: "#8a3030", color1: "#8a3032", icon: "", banner: "" },
    "11-23": { name: "Thanksgiving (US)", color0: "#ff9933", color1: "#663300", icon: "", banner: "" },
    "12-06": { name: "First Day of Hanukkah", color0: "#dead39", color1: "#dead39", icon: "", banner: "" },
    "12-21": { name: "First Day of Winter", color0: "#0e105d", color1: "#0e105d", icon: "", banner: "" },
    "12-24": { name: "Christmas Eve", color0: "#be312a", color1: "#be312a", icon: "", banner: "" },
    "12-25": { name: "Christmas Day", color0: "#008000", color1: "#ff0000", icon: "", banner: "" },
    "12-31": { name: "New Year's Eve", color0: "#d4aa45", color1: "#d4aa45", icon: "", banner: "" }
}



export const versionID = '26.10.01';
