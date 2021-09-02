require_relative 'portal/auth_client'
require_relative 'certificate_helper'
require_relative 'log'
require 'optparse'

begin
  options = {}
  OptionParser.new do |opt|
    opt.on('--subcommand SUBCOMMAND') { |o| options[:subcommand] = o }
  end.parse!

  Log.verbose = true

  Portal::AuthClient.login(apple_id, password)
  Log.info('logged in')

  case options[:subcommand]
  when 'list_dev_certs'
    client = CertificateHelper.new
    certificates = client.list_dev_certs
    result = { data: certificates }
    puts result.to_json.to_s
  end
rescue => e
  result = { error: "Error: #{e} Stacktrace: #{e.backtrace.join("\n")}" }
  puts result.to_json.to_s

  exit 1
end
