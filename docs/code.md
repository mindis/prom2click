### Code Organization / Brainstorming

## srv.Server
* have local reference to WriteWorker
* incoming http request on /write
* worker.Queue(request)
* on error http 500, otherwise http 200

## workers.WriteWorker
* reference to db, cache
* Queue => decompress etc and push to channel
* Run => dequeue from channel, batch send to db
* Wait => block caller until wait group is done

## DB.Conn
* operations to support remote write requests
    * caching of metric finger print => hash 
    * db select of metric hash values 
    * db creation and caching of metric hash values 
    * db insertion of metric samples


