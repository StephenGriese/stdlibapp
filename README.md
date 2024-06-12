This project is going to be an example of how to write a server and client that will pass jaeger spans

I will start by using kitty, to see how it works in Jaeger, and then will move to stdlib http

I decided to make a seperate project that uses kitty. 
Same project, just keeping kitty stuff. 
In this one, I will remove all kitty stuff.

# Metrics
For an idea on how to add "middleware" to the http.CLIENT, see the following:
https://stackoverflow.com/questions/39527847/is-there-middleware-for-go-http-client

The idea is to create a RoundTripper that will collect the statistics about the GET request.

