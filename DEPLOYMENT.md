## Description
The system described here is a REST API to get top Github contributors for a determined **City**. The Github REST API has a strong rate limites of api usage and the published schema does not provide an easy way to filter user contributions by user. This behavior enforces to query the api several times to get all the required information.

Given a location, github might return a huge amount of users, eg: Barcelona

## Deployment
Following there are two deployment models for this service explaining deployment considerations to take into account. The mau

### EKS (or other K8s platforms)
:DIAGRAM:

K8s is a good platform to run this system as it needs some interconnected components to run.
* High Availability: AWS Lambda provides regional deployments across multiple availability zones, this ensures a good failure tolerance against single zone failures. If the System needs to be deployed globally, the function could be deployed across multiple regions and load balance the trafic based on geographical location of the clients.

* Capacity: Lambda provides up to 1000 concurrent requests

The diagram described uses an AWS Api gateway along a AWS lambda function to deploy the service.
AWS Lambda provides regional high availability across multiple availability zones of a region. This ensures a good availablity in case of zone failures


### AWS EKS (Or other K8s platforms)
:DIAGRAM:

Using K8s with a regional group of nodes provides a high availability for the service as it can be deployed across multiple availability zones using node/pod affinities. K8s provides also good scaling policies based on resource metrics

