# What is it?

`metmar` is a trivial proxy to fetch French marine weather forecasts on north
Brittany area and reformat them in plain text. The motivation for this is
instead of grabbing megabytes of Javascript, banners and ads which obviously
fail to load in poor network conditions, you get a readable forecast in a
single GET request. Isn't that nice?

Data courtesy of Meteo France.

## Extra Services

The main service is run with "serve" command. Another service started with
"gale" scans a directory for saved weather forecasts, extract the gale warning
number if any and display it agains the day in the year. I am curious to see
how it evolves.
