# Concepts

## Customer

A customer is a recipient identified by an e-mail address and name. Customers receive e-mails that are sent from listmonk. A customer can be added to any number of customer_lists. Customers who are not a part of any customer_lists are considered *orphan* records.

### Attributes

Attributes are arbitrary properties attached to a customer in addition to their e-mail and name. They are represented as a JSON map. It is not necessary for all customers to have the same attributes. Customers can be [queried and segmented](querying-and-segmentation.md) into customer_lists based on their attributes, and the attributes can be inserted into the e-mails sent to them. For example:

```json
{
  "city": "Bengaluru",
  "likes_tea": true,
  "spoken_languages": ["English", "Malayalam"],
  "projects": 3,
  "stack": {
    "frameworks": ["echo", "go"],
    "languages": ["go", "python"],
    "preferred_language": "go"
  }
}
```

### Subscription statuses

A customer can be added to one or more customer_lists, and each such relationship can have one of these statuses.

| Status         | Description                                                                                                                                                                                 |
|----------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `unconfirmed`  | The customer was added to the customer_list directly without their explicit confirmation. Nonetheless, the customer will receive campaign messages sent to single opt-in campaigns.              |
| `confirmed`    | The customer confirmed their subscription by clicking on 'accept' in the confirmation e-mail. Only confirmed customers in opt-in customer_lists will receive campaign messages send to the customer_list. |
| `unsubscribed` | The customer is unsubscribed from the customer_list and will not receive any campaign messages sent to the customer_list.                                                                                   |

### Segmentation

Segmentation is the process of filtering a large list of customers into a smaller group based on arbitrary conditions, primarily based on their attributes. For instance, if an e-mail needs to be sent customers who live in a particular city, given their city is described in their attributes, it's possible to quickly filter them out into a new customer list and e-mail them. [Learn more](querying-and-segmentation.md).

## CustomerList

A customer_list (or a _mailing list_) is a collection of customers grouped under a name, for instance, _clients_. CustomerLists are used to organise customers and send e-mails to specific groups. A customer_list can be single opt-in or double opt-in. Customers added to double opt-in customer_lists have to explicitly accept the subscription by clicking on the confirmation e-mail they receive. Until then, they do not receive campaign messages.

## Campaign

A campaign is an e-mail (or any other kind of messages) that is sent to one or more customer_lists.


## Transactional message

A transactional message is an arbitrary message sent to a customer using the transactional message API. For example a welcome e-mail on signing up to a service; an order confirmation e-mail on purchasing an item; a password reset e-mail when a user initiates an online account recovery process.


## Template

A template is a re-usable HTML design that can be used across campaigns and when sending arbitrary transactional messages. Most commonly, templates have standard header and footer areas with logos and branding elements, where campaign content is inserted in the middle. listmonk supports [Go template](https://gowebexamples.com/templates/) expressions that lets you create powerful, dynamic HTML templates. [Learn more](templating.md).

## Messenger

listmonk supports multiple custom messaging backends in additional to the default SMTP e-mail backend, enabling not just e-mail campaigns, but arbitrary message campaigns such as SMS, FCM notifications etc. A *Messenger* is a web service that accepts a campaign message pushed to it as a JSON request, which the service can in turn broadcast as SMS, FCM etc. [Learn more](messengers.md).

## Tracking pixel

The tracking pixel is a tiny, invisible image that is inserted into an e-mail body to track e-mail views. This allows measuring the read rate of e-mails. While this is exceedingly common in e-mail campaigns, it carries privacy implications and should be used in compliance with rules and regulations such as GDPR. It is possible to track reads anonymously without associating an e-mail read to a customer.

## Click tracking

It is possible to track the clicks on every link that is sent in an e-mail. This allows measuring the clickthrough rates of links in e-mails. While this is exceedingly common in e-mail campaigns, it carries privacy implications and should be used in compliance with rules and regulations such as GDPR. It is possible to track link clicks anonymously without associating an e-mail read to a customer.

## Bounce

A bounce occurs when an e-mail that is sent to a recipient "bounces" back for one of many reasons including the recipient address being invalid, their mailbox being full, or the recipient's e-mail service provider marking the e-mail as spam. listmonk can automatically process such bounce e-mails that land in a configured POP mailbox, or via APIs of SMTP e-mail providers such as AWS SES and Sengrid. Based on settings, customers returning bounced e-mails can either be blocklisted or deleted automatically. [Learn more](bounces.md).
